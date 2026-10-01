package agent

import (
	"strings"
	"testing"

	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/permission"
)

func TestToMessagesSeparatesMemoryFromCurrentText(t *testing.T) {
	b := permission.Bundle{Blocks: []permission.Block{
		{Category: model.CatPersonality, Label: "personality", Text: "Ты — Юи."},
		{Category: model.CatPreferences, Label: "memory:semantic_fact", Text: "Любит чай"},
		{Category: model.CatCurrentText, Label: "current_text", Text: "что мне выпить?"},
	}}
	msgs := ToMessages(b)
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want system + user", len(msgs))
	}
	if msgs[0].Role != "system" || !strings.Contains(msgs[0].Content, "Любит чай") {
		t.Fatalf("system message missing recalled memory: %q", msgs[0].Content)
	}
	if msgs[1].Role != "user" || msgs[1].Content != "что мне выпить?" {
		t.Fatalf("user message = %+v", msgs[1])
	}
	if strings.Contains(msgs[0].Content, "что мне выпить?") {
		t.Fatal("current text must not be duplicated into the system message")
	}
}

func TestEndsSentence(t *testing.T) {
	if endsSentence("коротко") {
		t.Fatal("short fragments must not trigger synthesis")
	}
	if !endsSentence("Это достаточно длинное предложение.") {
		t.Fatal("a completed sentence should flush to TTS (VOICE-007)")
	}
}

func TestToMessagesKeepsDefaultLanguageWithoutPersonality(t *testing.T) {
	msgs := ToMessages(permission.Bundle{Blocks: []permission.Block{
		{Category: model.CatCurrentText, Label: "current_text", Text: "Привет"},
	}})
	if len(msgs) != 2 || msgs[0].Role != "system" || !strings.Contains(msgs[0].Content, "Отвечай по-русски") || !strings.Contains(msgs[0].Content, "явно не попросил другой язык") {
		t.Fatalf("default language must survive filtered personality context: %+v", msgs)
	}
}
