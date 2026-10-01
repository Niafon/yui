package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/yui-companion/core/internal/model"
)

func collect(t *testing.T, ch <-chan AudioChunk) []byte {
	t.Helper()
	var out []byte
	final := false
	for c := range ch {
		if c.Err != nil {
			t.Fatal(c.Err)
		}
		if len(c.Data)%2 != 0 {
			t.Fatalf("chunk splits a 16-bit sample: %d bytes", len(c.Data))
		}
		if c.SampleRate != cloudSampleRate {
			t.Fatalf("sample rate %d", c.SampleRate)
		}
		out = append(out, c.Data...)
		final = final || c.Final
	}
	if !final {
		t.Fatal("stream ended without a final chunk")
	}
	return out
}

func TestStreamPCMKeepsSamplesWhole(t *testing.T) {
	pcm := make([]byte, cloudChunkBytes*2+7)
	for i := range pcm {
		pcm[i] = byte(i)
	}
	out := make(chan AudioChunk, 64)
	// One byte per read exercises every odd boundary.
	go func() { defer close(out); streamPCM(context.Background(), iotest.OneByteReader(bytes.NewReader(pcm)), out) }()
	got := collect(t, out)
	if !bytes.Equal(got, pcm[:len(pcm)-1]) {
		t.Fatalf("got %d bytes, want %d (trailing half sample dropped)", len(got), len(pcm)-1)
	}
}

func TestCloudSpeechRequests(t *testing.T) {
	pcm := bytes.Repeat([]byte{1, 0}, 3000)
	for _, tc := range []struct {
		name, driver, model, voice, keyHeader, keyValue string
		check                                           func(t *testing.T, r *http.Request, body []byte)
	}{
		{"openai", "openai", "gpt-4o-mini-tts", "", "Authorization", "Bearer secret", func(t *testing.T, r *http.Request, body []byte) {
			if r.URL.Path != "/v1/audio/speech" {
				t.Fatalf("path %s", r.URL.Path)
			}
			var req map[string]any
			_ = json.Unmarshal(body, &req)
			if req["response_format"] != "pcm" || req["voice"] != "nova" || req["input"] != "Привет" || req["instructions"] == nil {
				t.Fatalf("request %v", req)
			}
		}},
		{"elevenlabs", "elevenlabs", "", "voice123", "xi-api-key", "secret", func(t *testing.T, r *http.Request, body []byte) {
			if r.URL.Path != "/v1/text-to-speech/voice123/stream" || r.URL.Query().Get("output_format") != "pcm_24000" {
				t.Fatalf("url %s", r.URL)
			}
			if !strings.Contains(string(body), `"model_id":"eleven_multilingual_v2"`) {
				t.Fatalf("body %s", body)
			}
		}},
		{"azure", "azure", "", "en-US-AvaNeural", "Ocp-Apim-Subscription-Key", "secret", func(t *testing.T, r *http.Request, body []byte) {
			if r.Header.Get("X-Microsoft-OutputFormat") != "raw-24khz-16bit-mono-pcm" {
				t.Fatalf("format header %q", r.Header.Get("X-Microsoft-OutputFormat"))
			}
			ssml := string(body)
			if !strings.Contains(ssml, `xml:lang="en-US"`) || !strings.Contains(ssml, `name="en-US-AvaNeural"`) || !strings.Contains(ssml, "&lt;b&gt;") {
				t.Fatalf("ssml %s", ssml)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if r.Header.Get(tc.keyHeader) != tc.keyValue {
					t.Errorf("credential header %s = %q", tc.keyHeader, r.Header.Get(tc.keyHeader))
				}
				tc.check(t, r, body)
				_, _ = w.Write(pcm)
			}))
			defer server.Close()
			endpoint := server.URL + "/v1"
			if tc.driver == "azure" {
				endpoint = server.URL + "/cognitiveservices/v1"
			}
			text := "Привет"
			if tc.driver == "azure" {
				text = "Привет <b>"
			}
			tts := NewCloudTTS(model.ProviderConfig{ID: tc.name, Kind: model.KindTTS, Driver: tc.driver, Endpoint: endpoint,
				Model: tc.model, Voice: tc.voice, APIKeyEnv: "KEY"}, "secret")
			ch, err := tts.Synthesize(context.Background(), SpeakRequest{Text: text, Voice: "xenia", Emotion: "joy"})
			if err != nil {
				t.Fatal(err)
			}
			if got := collect(t, ch); !bytes.Equal(got, pcm) {
				t.Fatalf("audio %d bytes, want %d", len(got), len(pcm))
			}
		})
	}
}

func TestCloudSpeechReportsHTTPErrorsAndMissingKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "quota exceeded", http.StatusTooManyRequests)
	}))
	defer server.Close()
	tts := NewCloudTTS(model.ProviderConfig{ID: "x", Kind: model.KindTTS, Driver: "openai", Endpoint: server.URL}, "")
	if _, err := tts.Synthesize(context.Background(), SpeakRequest{Text: "hi"}); err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("err = %v", err)
	}
	locked := NewCloudTTS(model.ProviderConfig{ID: "y", Kind: model.KindTTS, Driver: "elevenlabs", APIKeyEnv: "ELEVEN_KEY"}, "")
	if _, err := locked.Synthesize(context.Background(), SpeakRequest{Text: "hi"}); err == nil || !strings.Contains(err.Error(), "ELEVEN_KEY") {
		t.Fatalf("missing key err = %v", err)
	}
}

func TestRegistryBuildsSpeechDrivers(t *testing.T) {
	reg := NewRegistry()
	for _, c := range []model.ProviderConfig{
		{ID: "oa", Kind: model.KindTTS, Driver: "openai"},
		{ID: "el", Kind: model.KindTTS, Driver: "elevenlabs"},
		{ID: "az", Kind: model.KindTTS, Driver: "azure", Endpoint: "https://westeurope.tts.speech.microsoft.com/cognitiveservices/v1"},
	} {
		if err := reg.Register(c); err != nil {
			t.Fatalf("%s: %v", c.ID, err)
		}
		if _, _, err := reg.TTS(c.ID); err != nil {
			t.Fatalf("%s not materialised: %v", c.ID, err)
		}
	}
	if err := reg.Register(model.ProviderConfig{ID: "bad", Kind: model.KindLLM, Driver: "elevenlabs"}); err == nil {
		t.Fatal("ElevenLabs accepted as an LLM")
	}
}
