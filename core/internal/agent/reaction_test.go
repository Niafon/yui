package agent

import (
	"strings"
	"testing"
)

func TestReactionParserHandlesChunkBoundariesAndPlainText(t *testing.T) {
	var p ReactionParser
	if got := p.Feed("[emo"); got != "" {
		t.Fatalf("leaked prefix %q", got)
	}
	if got := p.Feed("tion:joy] Привет!"); got != "Привет!" {
		t.Fatalf("text %q", got)
	}
	if p.Emotion != "joy" || p.Flush() != "" {
		t.Fatalf("reaction %+v", p)
	}
	var plain ReactionParser
	if got := plain.Feed("[это текст]"); got != "[это текст]" {
		t.Fatalf("plain %q", got)
	}
	var invalid ReactionParser
	if got := invalid.Feed("[emotion:unknown] Ответ"); got != "Ответ" || invalid.Emotion != "" {
		t.Fatalf("invalid %+v text %q", invalid, got)
	}
	var malformed ReactionParser
	if got := malformed.Feed("[emotion:" + strings.Repeat("x", 50)); got != "" {
		t.Fatalf("malformed prefix leaked: %q", got)
	}
	if got := malformed.Feed("more] Ответ"); got != "Ответ" {
		t.Fatalf("malformed tail: %q", got)
	}
}
