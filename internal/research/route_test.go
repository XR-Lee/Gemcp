package research

import (
	"strings"
	"testing"
)

func TestMatchCodeRef(t *testing.T) {
	t.Parallel()
	cases := []struct {
		pattern string
		ref     string
		want    bool
	}{
		{pattern: "", ref: "main", want: true},
		{pattern: "", ref: "", want: true},
		{pattern: "autoresearch/*", ref: "", want: false},
		{pattern: "autoresearch/*", ref: "autoresearch/objbg-baseline", want: true},
		{pattern: "autoresearch/*", ref: "autoresearch/nested/more", want: false},
		{pattern: "autoresearch/*", ref: "main", want: false},
		{pattern: "autoresearch/*", ref: "research-plan", want: false},
		{pattern: "autoresearch/*", ref: "autoresearch", want: false},
		{pattern: "autoresearch/**", ref: "autoresearch/a/b", want: true},
		{pattern: "autoresearch/**", ref: "autoresearch", want: true},
		{pattern: "research-plan", ref: "research-plan", want: true},
		{pattern: "research-plan", ref: "main", want: false},
	}
	for _, tc := range cases {
		if got := MatchCodeRef(tc.pattern, tc.ref); got != tc.want {
			t.Fatalf("MatchCodeRef(%q, %q) = %v want %v", tc.pattern, tc.ref, got, tc.want)
		}
	}
}

func TestNormalizeRouteBindingRejectsFrozenRecipes(t *testing.T) {
	t.Parallel()
	if _, err := normalizeRouteBinding("research-plan", "gemcp.yaml", "autoresearch/*"); err == nil {
		t.Fatal("accepted gemcp.yaml as protocol_doc_path")
	}
	if _, err := normalizeRouteBinding("research-plan", "recipes/train.sh", "autoresearch/*"); err == nil {
		t.Fatal("accepted recipes/ as protocol_doc_path")
	}
	got, err := normalizeRouteBinding("research-plan", "research-plan/STATUS.md", "autoresearch/*")
	if err != nil || got.ProtocolBranch != "research-plan" || got.ProtocolDocPath != "research-plan/STATUS.md" || got.CodeRefPattern != "autoresearch/*" {
		t.Fatalf("normalizeRouteBinding() = %+v, %v", got, err)
	}
}

func TestRouteRefRejectedMessage(t *testing.T) {
	t.Parallel()
	if msg := RouteRefRejectedMessage("autoresearch/*", ""); !strings.Contains(msg, "explicit code ref") || !strings.Contains(msg, "autoresearch/*") {
		t.Fatalf("empty ref message = %q", msg)
	}
	if msg := RouteRefRejectedMessage("autoresearch/*", "main"); !strings.Contains(msg, "main") || !strings.Contains(msg, "autoresearch/*") {
		t.Fatalf("outside ref message = %q", msg)
	}
}
