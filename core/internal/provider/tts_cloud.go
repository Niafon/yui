package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/yui-companion/core/internal/model"
)

// Cloud speech synthesis. Every driver asks for raw 24 kHz 16-bit mono PCM so
// the stage can play and lip-sync chunks as they arrive, without decoding.
// Whether the reply text may leave the device is decided by the agent through
// the permission engine before Synthesize is called (SEC-004).

const cloudSampleRate = 24000

// cloudChunkBytes is 100 ms of audio: small enough to start playback early,
// large enough to keep frame overhead low.
const cloudChunkBytes = cloudSampleRate * 2 / 10

// Default voices when neither the provider config nor the identity names one
// the service understands.
var defaultVoices = map[string]string{
	"openai":     "nova",
	"elevenlabs": "21m00Tcm4TlvDq8ikWAM",
	"azure":      "ru-RU-SvetlanaNeural",
}

// Voice profiles that belong to local engines (Silero, Piper) and mean
// nothing to a cloud service.
var localVoiceProfiles = map[string]bool{
	"": true, "default": true, "xenia": true, "baya": true, "kseniya": true, "aidar": true, "eugene": true,
}

type cloudTTS struct {
	cfg    model.ProviderConfig
	apiKey string
	driver string
	http   *http.Client
}

// NewCloudTTS builds an OpenAI-compatible, ElevenLabs or Azure speech driver.
func NewCloudTTS(c model.ProviderConfig, apiKey string) TTS {
	return &cloudTTS{cfg: c, apiKey: apiKey, driver: c.Driver, http: &http.Client{Timeout: 2 * time.Minute}}
}

func (p *cloudTTS) Info() Info {
	return Info{ID: p.cfg.ID, Kind: model.KindTTS, Driver: p.driver, Model: p.cfg.Model, Local: p.cfg.Local, Streaming: true}
}

func (p *cloudTTS) voice(req SpeakRequest) string {
	if p.cfg.Voice != "" {
		return p.cfg.Voice
	}
	if v := strings.TrimSpace(req.Voice); !localVoiceProfiles[strings.ToLower(v)] {
		return v
	}
	return defaultVoices[p.driver]
}

func (p *cloudTTS) Synthesize(ctx context.Context, req SpeakRequest) (<-chan AudioChunk, error) {
	if strings.TrimSpace(req.Text) == "" {
		out := make(chan AudioChunk, 1)
		out <- AudioChunk{SampleRate: cloudSampleRate, Final: true}
		close(out)
		return out, nil
	}
	if p.cfg.APIKeyEnv != "" && strings.TrimSpace(p.apiKey) == "" {
		return nil, errors.New("provider " + p.cfg.ID + ": set " + p.cfg.APIKeyEnv + " and restart core")
	}
	httpReq, err := p.request(ctx, req)
	if err != nil {
		return nil, err
	}
	resp, err := p.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("%s tts: %s: %s", p.driver, resp.Status, strings.TrimSpace(string(detail)))
	}
	out := make(chan AudioChunk, 8)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		streamPCM(ctx, resp.Body, out)
	}()
	return out, nil
}

// streamPCM splits a PCM16 body into fixed chunks, never splitting a sample.
func streamPCM(ctx context.Context, body io.Reader, out chan<- AudioChunk) {
	buf := make([]byte, cloudChunkBytes)
	seq := 0
	pending := 0 // bytes already in buf from the previous read
	for {
		n, err := io.ReadFull(body, buf[pending:])
		total := pending + n
		even := total &^ 1
		if even > 0 {
			data := make([]byte, even)
			copy(data, buf[:even])
			select {
			case out <- AudioChunk{Data: data, SampleRate: cloudSampleRate, Seq: seq}:
				seq++
			case <-ctx.Done():
				return
			}
		}
		pending = total - even
		if pending == 1 {
			buf[0] = buf[even]
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
				out <- AudioChunk{Err: err, Final: true}
				return
			}
			out <- AudioChunk{SampleRate: cloudSampleRate, Seq: seq, Final: true}
			return
		}
	}
}

func (p *cloudTTS) request(ctx context.Context, req SpeakRequest) (*http.Request, error) {
	voice := p.voice(req)
	speed := req.Speed
	if speed <= 0 {
		speed = 1
	}
	switch p.driver {
	case "openai":
		body := map[string]any{
			"model": firstNonEmpty(p.cfg.Model, "gpt-4o-mini-tts"), "input": req.Text,
			"voice": voice, "response_format": "pcm", "speed": speed,
		}
		// Only steerable models accept free-form delivery instructions.
		if strings.Contains(p.cfg.Model, "gpt-4o") && req.Emotion != "" && req.Emotion != "neutral" {
			body["instructions"] = "Speak warmly and naturally; emotional tone: " + req.Emotion + "."
		}
		return p.jsonRequest(ctx, endpointJoin(p.cfg.Endpoint, "https://api.openai.com/v1", "/audio/speech"), body,
			map[string]string{"Authorization": "Bearer " + p.apiKey})
	case "elevenlabs":
		base := endpointJoin(p.cfg.Endpoint, "https://api.elevenlabs.io/v1", "/text-to-speech/"+url.PathEscape(voice)+"/stream")
		body := map[string]any{"text": req.Text, "model_id": firstNonEmpty(p.cfg.Model, "eleven_multilingual_v2")}
		return p.jsonRequest(ctx, base+"?output_format=pcm_24000", body, map[string]string{"xi-api-key": p.apiKey})
	case "azure":
		if p.cfg.Endpoint == "" {
			return nil, errors.New("azure tts: endpoint is required, e.g. https://westeurope.tts.speech.microsoft.com/cognitiveservices/v1")
		}
		lang := "ru-RU"
		if parts := strings.SplitN(voice, "-", 3); len(parts) == 3 {
			lang = parts[0] + "-" + parts[1]
		}
		rate := fmt.Sprintf("%+d%%", int((speed-1)*100))
		ssml := `<speak version="1.0" xmlns="http://www.w3.org/2001/10/synthesis" xml:lang="` + xmlEscape(lang) + `">` +
			`<voice name="` + xmlEscape(voice) + `"><prosody rate="` + rate + `">` + xmlEscape(req.Text) + `</prosody></voice></speak>`
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.Endpoint, strings.NewReader(ssml))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/ssml+xml")
		httpReq.Header.Set("X-Microsoft-OutputFormat", "raw-24khz-16bit-mono-pcm")
		httpReq.Header.Set("User-Agent", "yui-core")
		httpReq.Header.Set("Ocp-Apim-Subscription-Key", p.apiKey)
		return httpReq, nil
	}
	return nil, ErrUnsupported
}

func (p *cloudTTS) jsonRequest(ctx context.Context, target string, body any, headers map[string]string) (*http.Request, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		httpReq.Header.Set(key, value)
	}
	return httpReq, nil
}

func endpointJoin(configured, fallback, path string) string {
	base := strings.TrimRight(configured, "/")
	if base == "" {
		base = fallback
	}
	return base + path
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

var xmlReplacer = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")

func xmlEscape(s string) string { return xmlReplacer.Replace(s) }
