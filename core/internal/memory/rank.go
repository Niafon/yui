package memory

import (
	"math"
	"strings"
	"time"

	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/permission"
	"github.com/yui-companion/core/internal/provider"
)

// Scored is a retrieval candidate with its component scores, so the UI and the
// audit log can explain why a memory was recalled.
type Scored struct {
	Item     *model.MemoryItem `json:"item"`
	Score    float64           `json:"score"`
	Lexical  float64           `json:"lexical"`
	Semantic float64           `json:"semantic"`
	Recency  float64           `json:"recency"`
}

const recencyHalfLifeDays = 30.0

// statusWeight down-ranks anything not confirmed and removes anything that is
// no longer current (MEM-007, MEM-008).
func statusWeight(s model.MemoryStatus) float64 {
	switch s {
	case model.StatusConfirmed:
		return 1.0
	case model.StatusObserved, model.StatusExtracted:
		return 0.9
	case model.StatusExternal:
		return 0.8
	case model.StatusInferred:
		return 0.7
	case model.StatusAssumption, model.StatusNeedsConfirmation:
		return 0.5
	default:
		return 0.0
	}
}

// Lexical returns token overlap between query and content in 0..1.
func Lexical(queryTokens []string, content string) float64 {
	if len(queryTokens) == 0 {
		return 0
	}
	docTokens := provider.Tokenize(content)
	if len(docTokens) == 0 {
		return 0
	}
	docSet := make(map[string]bool, len(docTokens))
	for _, t := range docTokens {
		docSet[t] = true
	}
	hits := 0
	for _, t := range queryTokens {
		if docSet[t] {
			hits++
		}
	}
	return float64(hits) / float64(len(queryTokens))
}

// Recency decays with a 30 day half-life.
func Recency(occurredAt, now time.Time) float64 {
	age := now.Sub(occurredAt).Hours() / 24
	if age < 0 {
		age = 0
	}
	return math.Pow(0.5, age/recencyHalfLifeDays)
}

// Rank orders candidates for the context builder. Pinned memories get a fixed
// bonus and never fall out of the head of the list (MEM-011).
func Rank(query string, queryVec []float32, items []*model.MemoryItem, now time.Time, limit int) []Scored {
	qTokens := provider.Tokenize(query)
	out := make([]Scored, 0, len(items))
	for _, it := range items {
		if it == nil || it.DeletedAt != nil || (it.ExpiresAt != nil && !it.ExpiresAt.After(now)) {
			continue
		}
		w := statusWeight(it.Status)
		if w == 0 {
			continue
		}
		lex := Lexical(qTokens, it.Content)
		sem := 0.0
		if len(queryVec) > 0 && len(it.Embedding) == len(queryVec) {
			sem = provider.Cosine(queryVec, it.Embedding)
			if sem < 0 {
				sem = 0
			}
		}
		rec := Recency(it.OccurredAt, now)
		// Recency alone is not evidence that a memory answers this request.
		if !it.Pinned && len(qTokens) > 0 && lex == 0 && sem < 0.3 {
			continue
		}
		score := (0.40*lex + 0.30*sem + 0.20*clamp01(it.Importance) + 0.10*rec) * w * clamp01Min(it.Confidence, 0.2)
		if it.Pinned {
			score += 0.25
		}
		if score <= 0 {
			continue
		}
		out = append(out, Scored{Item: it, Score: score, Lexical: lex, Semantic: sem, Recency: rec})
	}
	sortScored(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// SelectContext keeps whole records and their provenance; it never invents a
// summary or silently truncates a fact. Diversity prevents repeated episodes
// from crowding a small model's context.
func SelectContext(ranked []Scored, limit, tokens int) []Scored {
	out := []Scored{}
	seen := map[string]bool{}
	used := 0
	for _, sc := range ranked {
		key := strings.Join(strings.Fields(strings.ToLower(sc.Item.Content)), " ")
		if key == "" || seen[key] {
			continue
		}
		cost := permission.EstimateTokens(sc.Item.Content) + 24
		if used+cost > tokens {
			continue
		}
		duplicate := false
		for _, prev := range out {
			if sc.Item.Category == prev.Item.Category && Lexical(provider.Tokenize(sc.Item.Content), prev.Item.Content) > 0.9 && Lexical(provider.Tokenize(prev.Item.Content), sc.Item.Content) > 0.9 {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		seen[key] = true
		used += cost
		out = append(out, sc)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func sortScored(s []Scored) {
	// insertion sort: retrieval sets are small and this keeps the ordering
	// stable for equal scores, which makes tests deterministic.
	for i := 1; i < len(s); i++ {
		cur := s[i]
		j := i - 1
		for j >= 0 && s[j].Score < cur.Score {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = cur
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func clamp01Min(v, min float64) float64 {
	v = clamp01(v)
	if v < min {
		return min
	}
	return v
}

// DecayedImportance implements gradual forgetting (SRS 10.9): importance falls
// with age unless the memory is pinned or confirmed by the owner.
func DecayedImportance(it *model.MemoryItem, now time.Time) float64 {
	if it.Pinned {
		return clamp01(it.Importance)
	}
	base := clamp01(it.Importance)
	factor := Recency(it.OccurredAt, now)
	if it.Status == model.StatusConfirmed {
		// Confirmed facts decay four times slower.
		factor = math.Pow(factor, 0.25)
	}
	return base * (0.4 + 0.6*factor)
}
