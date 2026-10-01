// Package ws implements the subset of RFC 6455 the core needs, without any
// third-party dependency. The core ships as a single binary (SRS 6), so the
// transport is deliberately small and auditable.
//
// Control plane and data plane use the same framing but separate endpoints
// (SRS 5.2): control traffic must never queue behind audio frames.
package ws

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const acceptMagic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// DefaultMaxPayload bounds a single frame (SRS 18.3: message size limits).
const DefaultMaxPayload int64 = 8 << 20 // 8 MiB

type OpCode byte

const (
	OpContinuation OpCode = 0x0
	OpText         OpCode = 0x1
	OpBinary       OpCode = 0x2
	OpClose        OpCode = 0x8
	OpPing         OpCode = 0x9
	OpPong         OpCode = 0xA
)

var (
	ErrNotWebSocket = errors.New("ws: not a websocket upgrade request")
	ErrNotHijacker  = errors.New("ws: response writer does not support hijacking")
	ErrClosed       = errors.New("ws: connection closed")
	ErrTooLarge     = errors.New("ws: frame exceeds payload limit")
)

// Conn is a single websocket connection. Writes are serialised; reads must be
// driven by one goroutine.
type Conn struct {
	raw        net.Conn
	br         *bufio.Reader
	wmu        sync.Mutex
	closed     bool
	maxPayload int64
	writeWait  time.Duration
}

// AcceptKey computes the Sec-WebSocket-Accept value for a client key.
func AcceptKey(clientKey string) string {
	h := sha1.New()
	io.WriteString(h, clientKey)
	io.WriteString(h, acceptMagic)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// IsUpgrade reports whether the request asks for a websocket upgrade.
func IsUpgrade(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	for _, tok := range strings.Split(r.Header.Get("Connection"), ",") {
		if strings.EqualFold(strings.TrimSpace(tok), "upgrade") {
			return true
		}
	}
	return false
}

// Upgrade completes the handshake and takes ownership of the TCP connection.
func Upgrade(w http.ResponseWriter, r *http.Request, maxPayload int64) (*Conn, error) {
	if !IsUpgrade(r) {
		return nil, ErrNotWebSocket
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, ErrNotWebSocket
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, ErrNotHijacker
	}
	raw, brw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + AcceptKey(key) + "\r\n\r\n"
	if _, err := brw.WriteString(resp); err != nil {
		raw.Close()
		return nil, err
	}
	if err := brw.Flush(); err != nil {
		raw.Close()
		return nil, err
	}
	if maxPayload <= 0 {
		maxPayload = DefaultMaxPayload
	}
	return &Conn{raw: raw, br: brw.Reader, maxPayload: maxPayload, writeWait: 15 * time.Second}, nil
}

// NewConn wraps an already established stream. Used by tests and by the
// desktop loopback transport.
func NewConn(raw net.Conn, maxPayload int64) *Conn {
	if maxPayload <= 0 {
		maxPayload = DefaultMaxPayload
	}
	return &Conn{raw: raw, br: bufio.NewReader(raw), maxPayload: maxPayload, writeWait: 15 * time.Second}
}

// ReadMessage returns the next complete application message. Ping/pong are
// handled transparently.
func (c *Conn) ReadMessage() (OpCode, []byte, error) {
	var (
		buf    []byte
		msgOp  = OpText
		opened bool
	)
	for {
		fin, op, payload, err := c.readFrame()
		if err != nil {
			return 0, nil, err
		}
		switch op {
		case OpPing:
			if err := c.write(OpPong, payload); err != nil {
				return 0, nil, err
			}
			continue
		case OpPong:
			continue
		case OpClose:
			_ = c.write(OpClose, payload)
			c.Close()
			return OpClose, payload, ErrClosed
		case OpContinuation:
			if !opened {
				return 0, nil, errors.New("ws: continuation without start frame")
			}
			buf = append(buf, payload...)
		case OpText, OpBinary:
			if opened {
				return 0, nil, errors.New("ws: interleaved message start")
			}
			opened = true
			msgOp = op
			buf = append(buf, payload...)
		default:
			return 0, nil, errors.New("ws: unsupported opcode")
		}
		if fin {
			return msgOp, buf, nil
		}
	}
}

func (c *Conn) readFrame() (bool, OpCode, []byte, error) {
	head := make([]byte, 2)
	if _, err := io.ReadFull(c.br, head); err != nil {
		return false, 0, nil, err
	}
	fin := head[0]&0x80 != 0
	op := OpCode(head[0] & 0x0F)
	masked := head[1]&0x80 != 0
	length := uint64(head[1] & 0x7F)
	switch length {
	case 126:
		ext := make([]byte, 2)
		if _, err := io.ReadFull(c.br, ext); err != nil {
			return false, 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext))
	case 127:
		ext := make([]byte, 8)
		if _, err := io.ReadFull(c.br, ext); err != nil {
			return false, 0, nil, err
		}
		length = binary.BigEndian.Uint64(ext)
	}
	if length > uint64(c.maxPayload) {
		return false, 0, nil, ErrTooLarge
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(c.br, mask[:]); err != nil {
			return false, 0, nil, err
		}
	}
	payload := make([]byte, int(length))
	if length > 0 {
		if _, err := io.ReadFull(c.br, payload); err != nil {
			return false, 0, nil, err
		}
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return fin, op, payload, nil
}

// WriteText sends a text frame.
func (c *Conn) WriteText(b []byte) error { return c.write(OpText, b) }

// WriteBinary sends a binary frame (audio and video chunks).
func (c *Conn) WriteBinary(b []byte) error { return c.write(OpBinary, b) }

// Ping keeps long voice sessions alive (VOICE-001).
func (c *Conn) Ping() error { return c.write(OpPing, nil) }

func (c *Conn) write(op OpCode, payload []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.closed {
		return ErrClosed
	}
	n := len(payload)
	var header []byte
	first := byte(0x80) | byte(op)
	switch {
	case n < 126:
		header = []byte{first, byte(n)}
	case n <= 0xFFFF:
		header = []byte{first, 126, byte(n >> 8), byte(n)}
	default:
		header = make([]byte, 10)
		header[0] = first
		header[1] = 127
		binary.BigEndian.PutUint64(header[2:], uint64(n))
	}
	_ = c.raw.SetWriteDeadline(time.Now().Add(c.writeWait))
	if _, err := c.raw.Write(header); err != nil {
		return err
	}
	if n > 0 {
		if _, err := c.raw.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

// Close sends a close frame once and releases the socket.
func (c *Conn) Close() error {
	c.wmu.Lock()
	if c.closed {
		c.wmu.Unlock()
		return nil
	}
	c.closed = true
	c.wmu.Unlock()
	return c.raw.Close()
}

// RemoteAddr exposes the peer address for audit records.
func (c *Conn) RemoteAddr() string { return c.raw.RemoteAddr().String() }

// EncodeFrame builds a client-style masked frame. Exported for tests and for
// the loopback client used by tooling.
func EncodeFrame(op OpCode, payload []byte, mask [4]byte) []byte {
	n := len(payload)
	out := []byte{0x80 | byte(op)}
	switch {
	case n < 126:
		out = append(out, 0x80|byte(n))
	case n <= 0xFFFF:
		out = append(out, 0x80|126, byte(n>>8), byte(n))
	default:
		ext := make([]byte, 8)
		binary.BigEndian.PutUint64(ext, uint64(n))
		out = append(out, 0x80|127)
		out = append(out, ext...)
	}
	out = append(out, mask[:]...)
	body := make([]byte, n)
	copy(body, payload)
	for i := range body {
		body[i] ^= mask[i%4]
	}
	return append(out, body...)
}
