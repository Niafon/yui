package ws

import (
	"net"
	"testing"
)

func TestAcceptKeyMatchesRFC6455Example(t *testing.T) {
	got := AcceptKey("dGhlIHNhbXBsZSBub25jZQ==")
	want := "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	if got != want {
		t.Fatalf("AcceptKey = %q, want %q", got, want)
	}
}

func TestReadMaskedTextFrame(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	conn := NewConn(server, DefaultMaxPayload)
	defer conn.Close()

	payload := []byte("привет, ядро")
	go func() {
		_, _ = client.Write(EncodeFrame(OpText, payload, [4]byte{1, 2, 3, 4}))
	}()

	op, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if op != OpText {
		t.Fatalf("opcode = %v, want text", op)
	}
	if string(data) != string(payload) {
		t.Fatalf("payload = %q, want %q", data, payload)
	}
}

func TestReadLargeFrameUsesExtendedLength(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	conn := NewConn(server, DefaultMaxPayload)
	defer conn.Close()

	payload := make([]byte, 70000) // forces the 64 bit length path
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	go func() {
		_, _ = client.Write(EncodeFrame(OpBinary, payload, [4]byte{9, 8, 7, 6}))
	}()

	op, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if op != OpBinary || len(data) != len(payload) {
		t.Fatalf("got op=%v len=%d, want binary len=%d", op, len(data), len(payload))
	}
	for i := range data {
		if data[i] != payload[i] {
			t.Fatalf("byte %d differs", i)
		}
	}
}

func TestFrameLargerThanLimitIsRejected(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	conn := NewConn(server, 128)
	defer conn.Close()

	go func() {
		_, _ = client.Write(EncodeFrame(OpBinary, make([]byte, 1024), [4]byte{1, 1, 1, 1}))
	}()
	if _, _, err := conn.ReadMessage(); err != ErrTooLarge {
		t.Fatalf("error = %v, want ErrTooLarge", err)
	}
}
