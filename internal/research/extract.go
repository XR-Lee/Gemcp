package research

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

const (
	maxExtractRefs        = 16
	maxExtractRows        = 64
	maxExtractRowsPerFile = 6
	defaultDynamicMamba   = "/Users/geeklee/Projects/Dynamicmamba/DynamicPointMamba"
	dynamicMambaSSH       = "git@github.com:XR-Lee/DynamicPointMamba.git"
)

var (
	catalogSourceFiles = []string{
		"experiment_graph.yaml",
		"SPRINT_G2_DECISION.md",
		"C1C2_OVERALL_RESULTS.md",
		"SAST4_SCRATCH_RESULTS.md",
		"M1M3_G0PRIME_RESULTS.md",
	}
	decimalMetricPattern = regexp.MustCompile(`[-+]?\d+\.\d+%?`)
	metricTokenPattern   = regexp.MustCompile(`[-+]?\d+(?:\.\d+)?%?`)
	markdownLinkRe       = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]+\)`)
	latexDollarRe        = regexp.MustCompile(`\$([^$]+)\$`)
)

type experimentGraphFile struct {
	Nodes []experimentGraphNode `yaml:"nodes"`
}

type experimentGraphNode struct {
	ID         string `yaml:"id"`
	Kind       string `yaml:"kind"`
	Title      string `yaml:"title"`
	When       string `yaml:"when"`
	Ref        string `yaml:"ref"`
	Conclusion string `yaml:"conclusion"`
}

func ExtractFromCheckout(dir string) ([]CatalogRowInput, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, invalid("checkout directory is required")
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return nil, err
	}
	refs, err := listResearchRefs(dir)
	if err != nil {
		return nil, err
	}
	rows := make([]CatalogRowInput, 0, 16)
	seen := map[string]struct{}{}
	for _, ref := range refs {
		if len(rows) >= maxExtractRows {
			break
		}
		sha, err := gitOutput(dir, "rev-parse", ref)
		if err != nil {
			continue
		}
		sha = strings.ToLower(strings.TrimSpace(sha))
		if !evidenceCommitPattern.MatchString(sha) {
			continue
		}
		branch := canonicalResearchRef(ref)
		for _, name := range catalogSourceFiles {
			if len(rows) >= maxExtractRows {
				break
			}
			content, err := gitOutput(dir, "show", sha+":"+name)
			if err != nil {
				continue
			}
			extracted := ParseCatalogSources(branch, sha, name, content)
			for _, row := range extracted {
				key := row.Branch + "\x00" + row.Hash + "\x00" + row.Setting
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				rows = append(rows, row)
				if len(rows) >= maxExtractRows {
					break
				}
			}
		}
	}
	return rows, nil
}

func ParseCatalogSources(branch, hash, filename, content string) []CatalogRowInput {
	filename = strings.TrimSpace(filename)
	switch {
	case strings.HasSuffix(strings.ToLower(filename), ".yaml"), strings.HasSuffix(strings.ToLower(filename), ".yml"):
		return parseExperimentGraphYAML(content)
	default:
		return parseResultMarkdown(branch, hash, filename, content)
	}
}

func parseExperimentGraphYAML(content string) []CatalogRowInput {
	var file experimentGraphFile
	if err := yaml.Unmarshal([]byte(content), &file); err != nil {
		return nil
	}
	rows := make([]CatalogRowInput, 0, len(file.Nodes))
	for _, node := range file.Nodes {
		branch, hash := parseGraphRef(node.Ref)
		if branch == "" || hash == "" {
			continue
		}
		conclusion := compactCatalogText(node.Conclusion)
		metric := firstMetricToken(conclusion)
		if metric == "" {
			continue
		}
		setting := compactCatalogText(strings.TrimSpace(node.Title + " " + node.When))
		if setting == "" {
			setting = compactCatalogText(node.ID)
		}
		method := compactCatalogText(node.Kind)
		if method == "" {
			method = "experiment"
		}
		implementation := compactCatalogText(node.Ref)
		if implementation == "" {
			implementation = "experiment_graph.yaml"
		}
		rows = append(rows, CatalogRowInput{
			Branch:         branch,
			Setting:        truncateCatalog(setting, maxCatalogSettingLength),
			Method:         truncateCatalog(method, maxCatalogMethodLength),
			Implementation: truncateCatalog(implementation, maxCatalogImplementationLen),
			Metric:         truncateCatalog(metric, maxCatalogMetricLength),
			Result:         truncateCatalog(conclusion, maxCatalogResultLength),
			Link:           "experiment_graph.yaml",
			Hash:           hash,
		})
	}
	return rows
}

func parseResultMarkdown(branch, hash, filename, content string) []CatalogRowInput {
	branch = canonicalResearchRef(branch)
	hash = strings.ToLower(strings.TrimSpace(hash))
	if branch == "" || hash == "" {
		return nil
	}
	rows := parseMarkdownMetricTables(branch, hash, filename, content)
	if summary := parseDecisionSummary(branch, hash, filename, content); summary != nil {
		rows = append([]CatalogRowInput{*summary}, rows...)
	}
	if len(rows) > maxExtractRowsPerFile {
		rows = rows[:maxExtractRowsPerFile]
	}
	return rows
}

func parseDecisionSummary(branch, hash, filename, content string) *CatalogRowInput {
	decision := ""
	reason := ""
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		switch {
		case strings.HasPrefix(lower, "decision:"):
			decision = compactCatalogText(trimmed[len("decision:"):])
		case strings.HasPrefix(lower, "reason:"):
			reason = compactCatalogText(trimmed[len("reason:"):])
		}
	}
	if decision == "" {
		return nil
	}
	metric := firstMetricToken(reason)
	if metric == "" {
		metric = firstMetricToken(decision)
	}
	if metric == "" {
		return nil
	}
	result := decision
	if reason != "" {
		result = decision + "; " + reason
	}
	return &CatalogRowInput{
		Branch:         branch,
		Setting:        truncateCatalog(strings.TrimSuffix(filename, filepath.Ext(filename)), maxCatalogSettingLength),
		Method:         truncateCatalog(decision, maxCatalogMethodLength),
		Implementation: truncateCatalog(filename, maxCatalogImplementationLen),
		Metric:         truncateCatalog(metric, maxCatalogMetricLength),
		Result:         truncateCatalog(result, maxCatalogResultLength),
		Link:           filename,
		Hash:           hash,
	}
}

func parseMarkdownMetricTables(branch, hash, filename, content string) []CatalogRowInput {
	lines := strings.Split(content, "\n")
	rows := []CatalogRowInput{}
	for i := 0; i < len(lines); i++ {
		header := splitMarkdownRow(lines[i])
		if len(header) < 2 || i+1 >= len(lines) || !isMarkdownDivider(lines[i+1]) {
			continue
		}
		meanIdx := columnIndex(header, "mean")
		armIdx := columnIndex(header, "arm")
		if armIdx < 0 {
			armIdx = columnIndex(header, "dataset")
		}
		accuracyIdx := columnIndex(header, "selected accuracy")
		if accuracyIdx < 0 {
			accuracyIdx = columnIndex(header, "best")
		}
		if meanIdx < 0 && accuracyIdx < 0 {
			continue
		}
		metricIdx := meanIdx
		if metricIdx < 0 {
			metricIdx = accuracyIdx
		}
		for j := i + 2; j < len(lines); j++ {
			cells := splitMarkdownRow(lines[j])
			if len(cells) == 0 {
				break
			}
			if len(cells) < len(header) {
				continue
			}
			label := ""
			if armIdx >= 0 && armIdx < len(cells) {
				label = cells[armIdx]
			}
			metric := ""
			if metricIdx >= 0 && metricIdx < len(cells) {
				metric = firstMetricToken(cells[metricIdx])
			}
			if metric == "" || strings.EqualFold(label, "task") {
				continue
			}
			setting := strings.TrimSuffix(filename, filepath.Ext(filename))
			if label != "" {
				setting = setting + " " + label
			}
			result := compactCatalogText(strings.Join(cells, " | "))
			rows = append(rows, CatalogRowInput{
				Branch:         branch,
				Setting:        truncateCatalog(setting, maxCatalogSettingLength),
				Method:         truncateCatalog(orCatalog(label, "reported"), maxCatalogMethodLength),
				Implementation: truncateCatalog(filename, maxCatalogImplementationLen),
				Metric:         truncateCatalog(metric, maxCatalogMetricLength),
				Result:         truncateCatalog(result, maxCatalogResultLength),
				Link:           filename,
				Hash:           hash,
			})
			if len(rows) >= maxExtractRowsPerFile {
				return rows
			}
		}
		i++
	}
	return rows
}

func parseGraphRef(ref string) (branch, hash string) {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.Contains(ref, " ") {
		return "", ""
	}
	ref = strings.TrimPrefix(ref, "tag:")
	ref = strings.TrimPrefix(ref, "refs/heads/")
	ref = strings.TrimPrefix(ref, "refs/tags/")
	ref = strings.TrimPrefix(ref, "origin/")
	if at := strings.LastIndex(ref, "@"); at >= 0 {
		branch = ref[:at]
		hash = strings.ToLower(ref[at+1:])
	} else {
		branch = ref
	}
	if !catalogBranchPattern.MatchString(branch) {
		return "", ""
	}
	if hash != "" && !evidenceCommitPattern.MatchString(hash) {
		return "", ""
	}
	return branch, hash
}

func listResearchRefs(dir string) ([]string, error) {
	output, err := gitOutput(dir, "for-each-ref", "--format=%(refname:short)", "refs/heads", "refs/remotes/origin", "refs/tags")
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	refs := make([]string, 0, 16)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name := canonicalResearchRef(line)
		if !isResearchRef(name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		refs = append(refs, line)
		if len(refs) >= maxExtractRefs {
			break
		}
	}
	return refs, nil
}

func isResearchRef(name string) bool {
	name = canonicalResearchRef(name)
	switch {
	case name == "research-plan":
		return true
	case strings.HasPrefix(name, "autoresearch/"):
		return true
	case strings.HasPrefix(name, "research/"):
		return true
	case strings.HasPrefix(name, "release/"):
		return true
	case strings.HasPrefix(name, "objbg-"):
		return true
	case strings.HasPrefix(name, "c1c2-"):
		return true
	case strings.HasPrefix(name, "sprint-"):
		return true
	default:
		return false
	}
}

func canonicalResearchRef(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, "refs/heads/")
	name = strings.TrimPrefix(name, "refs/tags/")
	name = strings.TrimPrefix(name, "refs/remotes/")
	name = strings.TrimPrefix(name, "origin/")
	name = strings.TrimPrefix(name, "tag:")
	return name
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func splitMarkdownRow(line string) []string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "|") {
		return nil
	}
	trimmed = strings.Trim(trimmed, "|")
	parts := strings.Split(trimmed, "|")
	cells := make([]string, 0, len(parts))
	for _, part := range parts {
		cells = append(cells, compactCatalogText(part))
	}
	return cells
}

func isMarkdownDivider(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "|") {
		return false
	}
	for _, cell := range splitMarkdownRow(trimmed) {
		for _, r := range cell {
			if r != '-' && r != ':' && !unicode.IsSpace(r) {
				return false
			}
		}
	}
	return true
}

func columnIndex(header []string, name string) int {
	name = strings.ToLower(strings.TrimSpace(name))
	for i, cell := range header {
		if strings.ToLower(cell) == name {
			return i
		}
	}
	return -1
}

func firstMetricToken(value string) string {
	value = compactCatalogText(value)
	if match := decimalMetricPattern.FindString(value); match != "" {
		return match
	}
	return metricTokenPattern.FindString(value)
}

func compactCatalogText(value string) string {
	value = markdownLinkRe.ReplaceAllString(value, "$1")
	value = latexDollarRe.ReplaceAllString(value, "$1")
	value = strings.ReplaceAll(value, "`", "")
	value = strings.ReplaceAll(value, "*", "")
	fields := strings.Fields(value)
	return strings.TrimSpace(strings.Join(fields, " "))
}

func truncateCatalog(value string, max int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max-1]) + "…"
}

func orCatalog(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func DynamicPointMambaCheckout() string {
	return defaultDynamicMamba
}

func DynamicPointMambaSSHURL() string {
	return dynamicMambaSSH
}
