package research

import (
	"path"
	"strings"
	"unicode/utf8"
)

const (
	maxProtocolBranchLength  = 255
	maxProtocolDocPathLength = 512
	maxCodeRefPatternLength  = 255
	defaultProtocolBranch    = "research-plan"
	defaultPlanSyncPath      = "research-plan/GEMCP-SYNC.md"
)

// RouteBinding names the docs-only protocol lane and the live code-ref family
// a Study (and its hypotheses) may prepare against.
type RouteBinding struct {
	ProtocolBranch  string `json:"protocol_branch,omitempty"`
	ProtocolDocPath string `json:"protocol_doc_path,omitempty"`
	CodeRefPattern  string `json:"code_ref_pattern,omitempty"`
}

func (r RouteBinding) Empty() bool {
	return strings.TrimSpace(r.ProtocolBranch) == "" &&
		strings.TrimSpace(r.ProtocolDocPath) == "" &&
		strings.TrimSpace(r.CodeRefPattern) == ""
}

// MatchCodeRef reports whether ref is allowed by pattern.
// An empty pattern allows any ref. `autoresearch/*` matches one path segment
// under that prefix; `autoresearch/**` matches nested refs. Exact strings match.
func MatchCodeRef(pattern, ref string) bool {
	pattern = strings.TrimSpace(pattern)
	ref = strings.TrimSpace(ref)
	if pattern == "" {
		return true
	}
	if ref == "" {
		return false
	}
	if pattern == ref {
		return true
	}
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return ref == prefix || strings.HasPrefix(ref, prefix+"/")
	}
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		if !strings.HasPrefix(ref, prefix+"/") {
			return false
		}
		rest := strings.TrimPrefix(ref, prefix+"/")
		return rest != "" && !strings.Contains(rest, "/")
	}
	ok, err := path.Match(pattern, ref)
	return err == nil && ok
}

func RouteRefRejectedMessage(pattern, ref string) string {
	pattern = strings.TrimSpace(pattern)
	if strings.TrimSpace(ref) == "" {
		return "this Study's route requires an explicit code ref matching " + pattern + "; omitting ref would bind the repository default branch"
	}
	return "requested ref " + strings.TrimSpace(ref) + " is outside this Study's allowed code ref family " + pattern
}

func normalizeRouteBinding(branch, docPath, pattern string) (RouteBinding, error) {
	protocolBranch, err := normalizeOptionalRouteRef("protocol_branch", branch, maxProtocolBranchLength, false)
	if err != nil {
		return RouteBinding{}, err
	}
	codePattern, err := normalizeOptionalRouteRef("code_ref_pattern", pattern, maxCodeRefPatternLength, true)
	if err != nil {
		return RouteBinding{}, err
	}
	doc, err := normalizeProtocolDocPath(docPath)
	if err != nil {
		return RouteBinding{}, err
	}
	return RouteBinding{ProtocolBranch: protocolBranch, ProtocolDocPath: doc, CodeRefPattern: codePattern}, nil
}

func normalizeOptionalRouteRef(field, value string, maxLength int, allowGlob bool) (string, error) {
	text := strings.TrimSpace(value)
	if text == "" {
		return "", nil
	}
	if utf8.RuneCountInString(text) > maxLength {
		return "", invalid(field + " is too long")
	}
	if strings.Contains(text, "..") || strings.Contains(text, "//") {
		return "", invalid(field + " is invalid")
	}
	for _, r := range text {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' || r == '/' {
			continue
		}
		if allowGlob && (r == '*' || r == '?') {
			continue
		}
		return "", invalid(field + " is invalid")
	}
	if text[0] == '/' || text[0] == '*' || text[0] == '?' || text[0] == '.' || text[0] == '-' {
		return "", invalid(field + " is invalid")
	}
	if err := rejectSecrets(text); err != nil {
		return "", err
	}
	return text, nil
}

func normalizeProtocolDocPath(value string) (string, error) {
	text := strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if text == "" {
		return "", nil
	}
	if utf8.RuneCountInString(text) > maxProtocolDocPathLength {
		return "", invalid("protocol_doc_path is too long")
	}
	if strings.HasPrefix(text, "/") || strings.Contains(text, "..") || strings.Contains(text, "//") {
		return "", invalid("protocol_doc_path must be a relative path without traversal")
	}
	if isFrozenRecipePath(text) {
		return "", invalid("protocol_doc_path cannot point at a frozen recipe file")
	}
	if err := rejectSecrets(text); err != nil {
		return "", err
	}
	return text, nil
}

func isFrozenRecipePath(value string) bool {
	cleaned := strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	base := path.Base(cleaned)
	switch strings.ToLower(base) {
	case "gemcp.yaml", "gemcp.yml", "dockerfile", "requirements.txt", "requirements.gemcp.txt":
		return true
	}
	return strings.HasPrefix(cleaned, "recipes/") || strings.HasPrefix(cleaned, "recipe/")
}

func routeFromStudy(protocolBranch, protocolDocPath, codeRefPattern string) *RouteBinding {
	binding := RouteBinding{
		ProtocolBranch:  strings.TrimSpace(protocolBranch),
		ProtocolDocPath: strings.TrimSpace(protocolDocPath),
		CodeRefPattern:  strings.TrimSpace(codeRefPattern),
	}
	if binding.Empty() {
		return nil
	}
	return &binding
}
