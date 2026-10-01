package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yui-companion/core/internal/model"
)

func TestWebSearchEncodesQueryAndReturnsSources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "Dota 2 патч & изменения" {
			t.Errorf("unexpected search query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<div class="result__body"><h2><a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fwww.dota2.com%2Fpatches%2F123&amp;rut=abc">Patch &amp; notes</a></h2><a class="result__snippet" href="#">&lt;b&gt;Heroes&lt;/b&gt; and items</a></div><div class="result__body"><a class="result__a" href="javascript:alert(1)">Bad</a></div>`))
	}))
	defer server.Close()

	result, err := searchWeb(context.Background(), server.Client(), server.URL, "Dota 2 патч & изменения")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Patch & notes", "https://www.dota2.com/patches/123", "Heroes and items"} {
		if !strings.Contains(result, want) {
			t.Fatalf("result missing %q: %s", want, result)
		}
	}
	if strings.Contains(result, "javascript:") || strings.Contains(result, "<b>") {
		t.Fatalf("unsafe result: %s", result)
	}
}

func TestWebSearchRejectsInvalidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>not a search results page</html>"))
	}))
	defer server.Close()
	if _, err := searchWeb(context.Background(), server.Client(), server.URL, "patch"); err == nil {
		t.Fatal("non-search response accepted as search result")
	}
	if _, err := searchWeb(context.Background(), server.Client(), server.URL, strings.Repeat("x", 301)); err == nil {
		t.Fatal("oversized query accepted")
	}
}

func TestWebSearchRequiresOwnerConfirmation(t *testing.T) {
	r := NewRegistry()
	if err := RegisterWebSearch(r); err != nil {
		t.Fatal(err)
	}
	tool, ok := r.Get("web.search")
	if !ok || tool.Risk != model.RiskMedium || tool.Category != model.CatCurrentText {
		t.Fatalf("unexpected web search policy: %+v", tool)
	}
}
