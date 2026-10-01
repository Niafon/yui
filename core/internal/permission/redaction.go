package permission

import (
	"context"
	"time"

	"github.com/yui-companion/core/internal/ids"
	"github.com/yui-companion/core/internal/model"
)

// Block is one labelled piece of context. Everything the agent wants to send
// to a model must be a Block, so nothing can bypass category filtering.
type Block struct {
	Category model.Category `json:"category"`
	Label    string         `json:"label"`
	Text     string         `json:"text"`
	// Tokens is an estimate used by the context budget (AI-011).
	Tokens int `json:"tokens"`
}

// Bundle is the ordered context for one model call.
type Bundle struct {
	Blocks []Block `json:"blocks"`
}

func (b Bundle) Tokens() int {
	n := 0
	for _, blk := range b.Blocks {
		n += blk.Tokens
	}
	return n
}

// Manifest is the audit record of what actually left the machine (contract C.4).
type Manifest struct {
	Provider         string           `json:"provider"`
	Purpose          string           `json:"purpose"`
	Included         []model.Category `json:"included_categories"`
	Excluded         []model.Category `json:"excluded_categories"`
	PolicyDecisionID string           `json:"policy_decision_id"`
	TraceID          string           `json:"trace_id"`
	Pending          []*Pending       `json:"-"`
	At               time.Time        `json:"at"`
}

// FilterForProvider removes every block the provider is not allowed to receive
// and returns the manifest for the audit log. This is the single choke point
// for outbound data (R-05).
func (e *Engine) FilterForProvider(ctx context.Context, p model.ProviderConfig, purpose string, in Bundle, traceID string) (Bundle, Manifest, error) {
	out := Bundle{}
	man := Manifest{
		Provider:         p.ID,
		Purpose:          purpose,
		PolicyDecisionID: ids.New("pol"),
		TraceID:          traceID,
		At:               time.Now().UTC(),
	}
	seenIn := map[model.Category]bool{}
	seenOut := map[model.Category]bool{}
	decisions := map[model.Category]Result{}

	for _, blk := range in.Blocks {
		res, checked := decisions[blk.Category]
		if !checked {
			var err error
			res, err = e.Check(ctx, Request{
				SubjectKind: model.SubjectProvider,
				SubjectID:   p.ID,
				Category:    blk.Category,
				Action:      model.ActionTransmit,
				Provider:    &p,
			})
			if err != nil {
				return Bundle{}, Manifest{}, err
			}
			decisions[blk.Category] = res
		}
		if res.Decision == model.DecisionAllow {
			out.Blocks = append(out.Blocks, blk)
			if !seenIn[blk.Category] {
				seenIn[blk.Category] = true
				man.Included = append(man.Included, blk.Category)
			}
			continue
		}
		if !seenOut[blk.Category] {
			seenOut[blk.Category] = true
			man.Excluded = append(man.Excluded, blk.Category)
		}
		if res.Decision == model.DecisionAsk && !checked {
			pend := e.RecordPending(Request{
				SubjectKind: model.SubjectProvider,
				SubjectID:   p.ID,
				Category:    blk.Category,
				Action:      model.ActionTransmit,
				Provider:    &p,
			}, model.ConfirmButton)
			man.Pending = append(man.Pending, pend)
		}
	}
	return out, man, nil
}

// Fit trims a bundle to a token budget, dropping the least important blocks
// first. Order of Priority: current text, then the tail of the list.
func Fit(in Bundle, budget int) Bundle {
	if budget <= 0 || in.Tokens() <= budget {
		return in
	}
	// Always keep the current user text.
	kept := Bundle{}
	used := 0
	for _, blk := range in.Blocks {
		if blk.Category == model.CatCurrentText {
			kept.Blocks = append(kept.Blocks, blk)
			used += blk.Tokens
		}
	}
	for _, blk := range in.Blocks {
		if blk.Category == model.CatCurrentText {
			continue
		}
		if used+blk.Tokens > budget {
			continue
		}
		kept.Blocks = append(kept.Blocks, blk)
		used += blk.Tokens
	}
	return kept
}

// EstimateTokens is a cheap approximation (4 characters per token) used for
// budgeting only. Providers report their real counts back in usage.
func EstimateTokens(s string) int {
	n := len([]rune(s)) / 4
	if n < 1 && len(s) > 0 {
		return 1
	}
	return n
}
