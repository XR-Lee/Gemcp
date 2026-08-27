package datasetcatalog

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/XR-Lee/Gemcp/internal/workspacecatalog"
)

const (
	BackendSSHCloud = "ssh_cloud"
	maxSources      = 32
)

var sha256Pattern = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)

var allowedSourceHosts = map[string]bool{
	"huggingface.co":                      true,
	"cdn-lfs.huggingface.co":              true,
	"objects.githubusercontent.com":       true,
	"github.com":                          true,
	"release-assets.githubusercontent.com": true,
}

var sshAllowedPrefixes = []string{"/root/", "/data/", "/home/", "/opt/", "/mnt/", "/gemcp/"}

type SourceFile struct {
	URL          string `json:"url" jsonschema:"HTTPS URL on an allowlisted host"`
	RelativePath string `json:"relative_path" jsonschema:"destination path relative to the canonical root"`
	SHA256       string `json:"sha256,omitempty" jsonschema:"optional SHA-256 hex digest of the downloaded bytes"`
}

type CatalogEntry struct {
	Name            string   `json:"name"`
	DisplayName     string   `json:"display_name"`
	Backend         string   `json:"backend"`
	CanonicalRoot   string   `json:"canonical_root"`
	RequiredMarkers []string `json:"required_markers"`
	Notes           string   `json:"notes"`
}

func Catalog() []CatalogEntry {
	return []CatalogEntry{{
		Name:          "scanobjectnn-objbg",
		DisplayName:   "ScanObjectNN OBJ-BG",
		Backend:       BackendElastic,
		CanonicalRoot: "/root/autodl-fs/datasets/ScanObjectNN",
		RequiredMarkers: []string{
			"main_split/training_objectdataset_augmentedrot_scale75.h5",
			"main_split/test_objectdataset_augmentedrot_scale75.h5",
		},
		Notes: "For Public Elastic, register this catalog name with HTTPS Hugging Face resolve URLs as sources, then prepare_experiment with runtime_preset=provision. Do not use register_workspace_dataset; that tool only declares paths under an Owner-approved Self-hosted workspace.",
	}}
}

func LookupCatalog(name string) (CatalogEntry, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, entry := range Catalog() {
		if entry.Name == name {
			return entry, true
		}
	}
	return CatalogEntry{}, false
}

func NormalizeSources(values []SourceFile) ([]SourceFile, error) {
	if len(values) > maxSources {
		return nil, invalid("sources is limited to 32 HTTPS files")
	}
	result := make([]SourceFile, 0, len(values))
	seenPath := map[string]bool{}
	for _, value := range values {
		normalized, err := NormalizeSource(value)
		if err != nil {
			return nil, err
		}
		if seenPath[normalized.RelativePath] {
			return nil, invalid("sources relative_path values must be unique")
		}
		seenPath[normalized.RelativePath] = true
		result = append(result, normalized)
	}
	return result, nil
}

func NormalizeSource(value SourceFile) (SourceFile, error) {
	parsed, err := url.Parse(strings.TrimSpace(value.URL))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
		return SourceFile{}, invalid("source URL must be a credential-free HTTPS URL without a query or fragment")
	}
	if !sourceHostAllowed(parsed.Hostname()) {
		return SourceFile{}, invalid("source URL host is not allowlisted")
	}
	relativePath, err := workspacecatalog.NormalizeRelativePath(value.RelativePath)
	if err != nil {
		return SourceFile{}, invalid("source relative_path must be a normalized relative file under the canonical root")
	}
	digest := strings.ToLower(strings.TrimSpace(value.SHA256))
	if digest != "" && !sha256Pattern.MatchString(digest) {
		return SourceFile{}, invalid("source sha256 must be a 64-character hex digest")
	}
	return SourceFile{URL: parsed.String(), RelativePath: relativePath, SHA256: digest}, nil
}

func sourceHostAllowed(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if allowedSourceHosts[host] {
		return true
	}
	return strings.HasSuffix(host, ".huggingface.co") && strings.HasPrefix(host, "cdn-lfs")
}

func SourcesFromRecord(values []map[string]string) []SourceFile {
	result := make([]SourceFile, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		result = append(result, SourceFile{
			URL: value["url"], RelativePath: value["relative_path"], SHA256: value["sha256"],
		})
	}
	return result
}

func SourceMaps(values []SourceFile) []map[string]string {
	result := make([]map[string]string, 0, len(values))
	for _, value := range values {
		item := map[string]string{"url": value.URL, "relative_path": value.RelativePath}
		if value.SHA256 != "" {
			item["sha256"] = value.SHA256
		}
		result = append(result, item)
	}
	return result
}

func HasSources(values []SourceFile) bool { return len(values) > 0 }
