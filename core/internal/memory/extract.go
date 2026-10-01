package memory

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/provider"
)

// The extractor runs in two tiers. Cheap deterministic rules run on every turn
// so the obvious facts are never missed and cost nothing; the LLM pass runs in
// the background scheduler, where latency does not hurt the conversation
// (SRS 7.3, 12.4).

type rule struct {
	re          *regexp.Regexp
	subject     string
	category    model.Category
	sensitivity model.Sensitivity
	confidence  float64
	importance  float64
	template    string
}

var rules = []rule{
	{
		re: regexp.MustCompile(`(?i)(?:меня зовут|моё имя|мое имя|my name is)\s+([\p{L}\-]{2,40})`),
		subject: "user.name", category: model.CatProfile, sensitivity: model.SensPrivate,
		confidence: 0.92, importance: 0.9, template: "Пользователя зовут %s",
	},
	{
		re: regexp.MustCompile(`(?i)(?:я живу в|я нахожусь в|i live in)\s+([\p{L}\s\-]{2,60})`),
		subject: "user.city", category: model.CatPlaces, sensitivity: model.SensPrivate,
		confidence: 0.85, importance: 0.7, template: "Пользователь живёт в %s",
	},
	{
		re: regexp.MustCompile(`(?i)(?:я работаю|i work as|моя работа)\s+([\p{L}\s\-]{2,60})`),
		subject: "user.occupation", category: model.CatProfile, sensitivity: model.SensPrivate,
		confidence: 0.8, importance: 0.7, template: "Работа пользователя: %s",
	},
	{
		re: regexp.MustCompile(`(?i)(?:мой любимый|моя любимая|моё любимое|мое любимое)\s+([\p{L}\s\-]{2,40})\s+(?:это|—|-)\s+([\p{L}\s\-\d]{2,60})`),
		subject: "", category: model.CatPreferences, sensitivity: model.SensNormal,
		confidence: 0.8, importance: 0.6, template: "Любимый %s: %s",
	},
	{
		re: regexp.MustCompile(`(?i)(?:^|\s)(?:я )?(?:люблю|мне нравится|i like|i love)\s+([\p{L}\s\-\d]{2,60})`),
		subject: "", category: model.CatPreferences, sensitivity: model.SensNormal,
		confidence: 0.7, importance: 0.5, template: "Пользователю нравится %s",
	},
	{
		re: regexp.MustCompile(`(?i)(?:я не люблю|мне не нравится|терпеть не могу|i don't like|i hate)\s+([\p{L}\s\-\d]{2,60})`),
		subject: "", category: model.CatPreferences, sensitivity: model.SensNormal,
		confidence: 0.7, importance: 0.5, template: "Пользователю не нравится %s",
	},
	{
		re: regexp.MustCompile(`(?i)(?:запомни,?\s*что|запомни:|remember that)\s+(.{3,200})`),
		subject: "", category: model.CatPreferences, sensitivity: model.SensNormal,
		confidence: 0.95, importance: 0.85, template: "%s",
	},
}

type Extractor struct {
	reg *provider.Registry
}

func NewExtractor(reg *provider.Registry) *Extractor { return &Extractor{reg: reg} }

// FromTurn applies the deterministic rules. It never calls a model, so it is
// safe on the latency path.
func (e *Extractor) FromTurn(identityID, spaceID, text string, prov model.Provenance) []model.MemoryCandidate {
	out := []model.MemoryCandidate{}
	seen := map[string]bool{}
	for _, r := range rules {
		m := r.re.FindStringSubmatch(text)
		if m == nil {
			continue
		}
		content := renderTemplate(r.template, m[1:])
		key := string(r.category) + "|" + strings.ToLower(content)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, model.MemoryCandidate{
			IdentityID:        identityID,
			SpaceID:           spaceID,
			Type:              model.MemSemanticFact,
			Category:          r.category,
			Subject:           r.subject,
			NormalizedContent: content,
			Confidence:        r.confidence,
			Importance:        r.importance,
			Sensitivity:       r.sensitivity,
			Provenance:        []model.Provenance{prov},
			RequiresConfirmation: r.confidence < 0.75,
		})
	}
	return out
}

func renderTemplate(tpl string, args []string) string {
	out := tpl
	for _, a := range args {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		out = strings.Replace(out, "%s", a, 1)
	}
	return strings.TrimSpace(strings.ReplaceAll(out, "%s", ""))
}

const extractionPrompt = `Ты — модуль извлечения памяти. Верни ТОЛЬКО JSON-массив без пояснений.
Каждый элемент: {"content": "утверждение о пользователе", "category": "profile|preferences|projects|calendar|places|contacts", "subject": "стабильный ключ или пустая строка", "confidence": 0..1, "importance": 0..1}.
Извлекай только устойчивые факты, а не реплики диалога. Если фактов нет — верни [].

Диалог:
`

// FromLLM asks the configured model for additional candidates. Used by the
// background reflection job, not by the live turn.
func (e *Extractor) FromLLM(ctx context.Context, providerID, identityID, spaceID, transcript string, prov model.Provenance) ([]model.MemoryCandidate, error) {
	llm, _, err := e.reg.LLM(providerID)
	if err != nil {
		return nil, err
	}
	stream, err := llm.Chat(ctx, provider.ChatRequest{
		Messages: []provider.Message{
			{Role: "system", Content: extractionPrompt},
			{Role: "user", Content: transcript},
		},
		Temperature: 0,
		MaxTokens:   800,
	})
	if err != nil {
		return nil, err
	}
	var sb strings.Builder
	for d := range stream {
		if d.Err != nil {
			return nil, d.Err
		}
		sb.WriteString(d.Text)
	}
	return parseCandidates(sb.String(), identityID, spaceID, prov)
}

func parseCandidates(raw, identityID, spaceID string, prov model.Provenance) ([]model.MemoryCandidate, error) {
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start < 0 || end <= start {
		return nil, nil
	}
	var parsed []struct {
		Content    string  `json:"content"`
		Category   string  `json:"category"`
		Subject    string  `json:"subject"`
		Confidence float64 `json:"confidence"`
		Importance float64 `json:"importance"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &parsed); err != nil {
		return nil, err
	}
	out := make([]model.MemoryCandidate, 0, len(parsed))
	for _, p := range parsed {
		if strings.TrimSpace(p.Content) == "" {
			continue
		}
		cat := model.Category(p.Category)
		if !validCategory(cat) {
			cat = model.CatPreferences
		}
		conf := p.Confidence
		if conf <= 0 || conf > 1 {
			conf = 0.6
		}
		out = append(out, model.MemoryCandidate{
			IdentityID:        identityID,
			SpaceID:           spaceID,
			Type:              model.MemSemanticFact,
			Category:          cat,
			Subject:           strings.TrimSpace(p.Subject),
			NormalizedContent: strings.TrimSpace(p.Content),
			Confidence:        conf,
			Importance:        clamp01(p.Importance),
			Sensitivity:       model.SensNormal,
			Provenance:        []model.Provenance{prov},
			// Model-derived facts are never treated as confirmed (R-04).
			RequiresConfirmation: true,
		})
	}
	return out, nil
}

func validCategory(c model.Category) bool {
	for _, known := range model.AllCategories() {
		if known == c {
			return true
		}
	}
	return false
}
