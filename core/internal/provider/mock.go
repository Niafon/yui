package provider

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
	"time"

	"github.com/yui-companion/core/internal/model"
)

// The mock drivers make the whole system runnable with no model installed.
// They are also the reference implementation used by contract tests: any real
// driver must behave the same way from the core's point of view (AI-001).

type MockLLM struct{ cfg model.ProviderConfig }

func NewMockLLM(c model.ProviderConfig) *MockLLM { return &MockLLM{cfg: c} }

func (m *MockLLM) Info() Info {
	return Info{ID: m.cfg.ID, Kind: model.KindLLM, Driver: "mock", Model: "mock-1",
		Local: true, Streaming: true, Tools: false, ContextTokens: 8192}
}

func (m *MockLLM) Chat(ctx context.Context, req ChatRequest) (<-chan Delta, error) {
	out := make(chan Delta, 16)
	go func() {
		defer close(out)
		reply := mockReply(req.Messages)
		for _, word := range strings.Fields(reply) {
			select {
			case <-ctx.Done():
				out <- Delta{Err: ctx.Err(), Done: true}
				return
			case out <- Delta{Text: word + " "}:
			}
			time.Sleep(8 * time.Millisecond)
		}
		out <- Delta{Done: true, Usage: &Usage{
			PromptTokens:     len(req.Messages) * 16,
			CompletionTokens: len(strings.Fields(reply)),
		}}
	}()
	return out, nil
}

func mockReply(msgs []Message) string {
	var lastUser, facts string
	for _, m := range msgs {
		switch m.Role {
		case "user":
			lastUser = m.Content
		case "system":
			if strings.Contains(m.Content, "memory") || strings.Contains(m.Content, "память") {
				facts = m.Content
			}
		}
	}
	b := &strings.Builder{}
	b.WriteString("[mock] ")
	if lastUser != "" {
		b.WriteString("Услышала: ")
		b.WriteString(truncate(lastUser, 160))
		b.WriteString(". ")
	}
	if facts != "" {
		b.WriteString("Учитываю сохранённый контекст. ")
	}
	b.WriteString("Подключите локальную модель, чтобы получить настоящий ответ.")
	return b.String()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

type MockSTT struct{ cfg model.ProviderConfig }

func NewMockSTT(c model.ProviderConfig) *MockSTT { return &MockSTT{cfg: c} }

func (m *MockSTT) Info() Info {
	return Info{ID: m.cfg.ID, Kind: model.KindSTT, Driver: "mock", Local: true, Streaming: true}
}

// Transcribe returns a placeholder transcript describing the audio it got, so
// the pipeline can be exercised end to end without a speech model.
func (m *MockSTT) Transcribe(_ context.Context, req TranscribeRequest) (Transcript, error) {
	owner := true
	return Transcript{
		Text:           "[mock stt] " + humanDuration(len(req.Audio), req.SampleRate, req.Channels),
		Confidence:     0.5,
		Final:          req.Final,
		SpeakerIsOwner: &owner,
	}, nil
}

func humanDuration(bytes, rate, channels int) string {
	if rate <= 0 {
		rate = 16000
	}
	if channels <= 0 {
		channels = 1
	}
	seconds := float64(bytes) / float64(rate*channels*2)
	return strings.TrimRight(strings.TrimRight(formatFloat(seconds), "0"), ".") + "s audio"
}

func formatFloat(f float64) string {
	f = math.Round(f*100) / 100
	whole := int(f)
	frac := int(math.Round((f - float64(whole)) * 100))
	return itoa(whole) + "." + pad2(frac)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

func pad2(i int) string {
	if i < 10 {
		return "0" + itoa(i)
	}
	return itoa(i)
}

type MockTTS struct{ cfg model.ProviderConfig }

func NewMockTTS(c model.ProviderConfig) *MockTTS { return &MockTTS{cfg: c} }

func (m *MockTTS) Info() Info {
	return Info{ID: m.cfg.ID, Kind: model.KindTTS, Driver: "mock", Local: true, Streaming: true}
}

// Synthesize emits silent PCM frames at roughly real time so clients can test
// buffering and cancellation.
func (m *MockTTS) Synthesize(ctx context.Context, req SpeakRequest) (<-chan AudioChunk, error) {
	out := make(chan AudioChunk, 8)
	go func() {
		defer close(out)
		frames := len([]rune(req.Text))/8 + 1
		for i := 0; i < frames; i++ {
			select {
			case <-ctx.Done():
				out <- AudioChunk{Err: ctx.Err(), Final: true}
				return
			case out <- AudioChunk{Data: make([]byte, 3200), SampleRate: 16000, Seq: i}:
			}
			time.Sleep(20 * time.Millisecond)
		}
		out <- AudioChunk{SampleRate: 16000, Seq: frames, Final: true}
	}()
	return out, nil
}

type MockVision struct{ cfg model.ProviderConfig }

func NewMockVision(c model.ProviderConfig) *MockVision { return &MockVision{cfg: c} }

func (m *MockVision) Info() Info {
	return Info{ID: m.cfg.ID, Kind: model.KindVision, Driver: "mock", Local: true, Vision: true}
}

func (m *MockVision) Analyze(_ context.Context, req VisionRequest) (VisionResult, error) {
	return VisionResult{
		Description: "[mock vision] кадр " + itoa(len(req.Image)) + " байт, тип " + req.MIME,
		Confidence:  0.3,
	}, nil
}

// HashEmbeddings is a deterministic, dependency-free embedding used for local
// retrieval before a real embedding model is configured. The Python worker
// implements the identical algorithm so vectors stay comparable (SRS 17.4).
type HashEmbeddings struct {
	cfg model.ProviderConfig
	dim int
}

func NewHashEmbeddings(c model.ProviderConfig, dim int) *HashEmbeddings {
	if dim <= 0 {
		dim = 256
	}
	return &HashEmbeddings{cfg: c, dim: dim}
}

func (h *HashEmbeddings) Info() Info {
	return Info{ID: h.cfg.ID, Kind: model.KindEmbeddings, Driver: "mock", Local: true, Model: "hash-" + itoa(h.dim)}
}

func (h *HashEmbeddings) Dimensions() int { return h.dim }

func (h *HashEmbeddings) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for _, t := range texts {
		out = append(out, HashVector(t, h.dim))
	}
	return out, nil
}

// HashVector maps text to a normalised sparse vector using FNV-1a token
// hashing. Exported because the retrieval tests and the Python worker rely on
// the exact algorithm.
func HashVector(text string, dim int) []float32 {
	vec := make([]float32, dim)
	for _, tok := range Tokenize(text) {
		hsh := fnv.New32a()
		hsh.Write([]byte(tok))
		sum := hsh.Sum32()
		idx := int(sum % uint32(dim))
		if sum&1 == 0 {
			vec[idx] += 1
		} else {
			vec[idx] -= 1
		}
	}
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	if norm == 0 {
		return vec
	}
	norm = math.Sqrt(norm)
	for i := range vec {
		vec[i] = float32(float64(vec[i]) / norm)
	}
	return vec
}

// Tokenize lowercases and splits on everything that is not a letter or digit.
// It is shared by retrieval scoring so ranking and embedding agree.
func Tokenize(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !isLetterOrDigit(r)
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if len([]rune(f)) > 1 {
			out = append(out, f)
		}
	}
	return out
}

func isLetterOrDigit(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		return true
	case r >= 'а' && r <= 'я', r == 'ё':
		return true
	}
	return false
}

// Cosine returns the similarity of two equal-length vectors.
func Cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
