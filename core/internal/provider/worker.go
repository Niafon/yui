package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yui-companion/core/internal/model"
)

// WorkerClient talks to a Python AI worker over loopback JSON-RPC (SRS 18.3).
// The worker receives only the task payload — never database access and never
// the whole context (VIS-009).
type WorkerClient struct {
	cfg  model.ProviderConfig
	http *http.Client
	dim  int
}

func NewWorkerClient(c model.ProviderConfig) *WorkerClient {
	return &WorkerClient{cfg: c, http: &http.Client{Timeout: 3 * time.Minute}}
}

func (w *WorkerClient) Info() Info {
	return Info{
		ID: w.cfg.ID, Kind: w.cfg.Kind, Driver: "worker", Model: w.cfg.Model,
		Local: true, Streaming: w.cfg.Kind == model.KindTTS,
		Vision: w.cfg.Kind == model.KindVision,
	}
}

func (w *WorkerClient) call(ctx context.Context, op string, in any, out any) error {
	base := strings.TrimRight(w.cfg.Endpoint, "/")
	if base == "" {
		return ErrNotConfigured
	}
	buf, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/"+op, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return errors.New("worker " + w.cfg.ID + ": " + resp.Status + ": " + string(snippet))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Health is polled by the supervisor (CORE-005, CORE-007).
func (w *WorkerClient) Health(ctx context.Context) error {
	base := strings.TrimRight(w.cfg.Endpoint, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := w.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("worker " + w.cfg.ID + " unhealthy: " + resp.Status)
	}
	return nil
}

func (w *WorkerClient) Transcribe(ctx context.Context, req TranscribeRequest) (Transcript, error) {
	in := map[string]any{
		"session_id":  req.SessionID,
		"audio_b64":   base64.StdEncoding.EncodeToString(req.Audio),
		"sample_rate": req.SampleRate,
		"channels":    req.Channels,
		"format":      req.Format,
		"language":    req.Language,
		"final":       req.Final,
		"model":       w.cfg.Model,
	}
	var out Transcript
	if err := w.call(ctx, "stt", in, &out); err != nil {
		return Transcript{}, err
	}
	return out, nil
}

type ttsResponse struct {
	AudioB64   string `json:"audio_b64"`
	SampleRate int    `json:"sample_rate"`
	Chunks     []struct {
		AudioB64 string `json:"audio_b64"`
		Seq      int    `json:"seq"`
	} `json:"chunks"`
}

// Synthesize converts the worker's chunk list into a stream. Workers that
// return a single blob still stream to the client in one chunk.
func (w *WorkerClient) Synthesize(ctx context.Context, req SpeakRequest) (<-chan AudioChunk, error) {
	in := map[string]any{
		"text":    req.Text,
		"voice":   req.Voice,
		"emotion": req.Emotion,
		"speed":   req.Speed,
		"format":  req.Format,
		"model":   w.cfg.Model,
	}
	var resp ttsResponse
	if err := w.call(ctx, "tts", in, &resp); err != nil {
		return nil, err
	}
	out := make(chan AudioChunk, 8)
	go func() {
		defer close(out)
		rate := resp.SampleRate
		if rate == 0 {
			rate = 16000
		}
		if len(resp.Chunks) > 0 {
			for _, c := range resp.Chunks {
				data, err := base64.StdEncoding.DecodeString(c.AudioB64)
				if err != nil {
					out <- AudioChunk{Err: err, Final: true}
					return
				}
				select {
				case out <- AudioChunk{Data: data, SampleRate: rate, Seq: c.Seq}:
				case <-ctx.Done():
					return
				}
			}
		} else if resp.AudioB64 != "" {
			data, err := base64.StdEncoding.DecodeString(resp.AudioB64)
			if err != nil {
				out <- AudioChunk{Err: err, Final: true}
				return
			}
			out <- AudioChunk{Data: data, SampleRate: rate, Seq: 0}
		}
		out <- AudioChunk{SampleRate: rate, Final: true}
	}()
	return out, nil
}

func (w *WorkerClient) Analyze(ctx context.Context, req VisionRequest) (VisionResult, error) {
	in := map[string]any{
		"image_b64": base64.StdEncoding.EncodeToString(req.Image),
		"mime":      req.MIME,
		"question":  req.Question,
		"want_ocr":  req.WantOCR,
		"model":     w.cfg.Model,
	}
	var out VisionResult
	if err := w.call(ctx, "vision", in, &out); err != nil {
		return VisionResult{}, err
	}
	return out, nil
}

func (w *WorkerClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return w.EmbedFor(ctx, texts, "")
}

func (w *WorkerClient) EmbedFor(ctx context.Context, texts []string, purpose string) ([][]float32, error) {
	in := map[string]any{"texts": texts, "model": w.cfg.Model, "purpose": purpose}
	var out struct {
		Vectors [][]float32 `json:"vectors"`
		Dim     int         `json:"dim"`
	}
	if err := w.call(ctx, "embeddings", in, &out); err != nil {
		return nil, err
	}
	if out.Dim > 0 {
		w.dim = out.Dim
	}
	return out.Vectors, nil
}

func (w *WorkerClient) Dimensions() int { return w.dim }
