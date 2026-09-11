package datasetcatalog

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/XR-Lee/Gemcp/internal/workspacecatalog"
)

var ErrRemoteMarkers = errors.New("required markers cannot be probed from the control plane")

func CanProbeMarkers(canonicalRoot string) bool {
	root := strings.TrimSpace(canonicalRoot)
	if root == "" {
		return false
	}
	cleaned := filepath.Clean(root)
	return !strings.HasPrefix(cleaned, "/root/autodl-fs/")
}

func MarkerPath(canonicalRoot, marker string) (string, error) {
	root := filepath.Clean(strings.TrimSpace(canonicalRoot))
	if root == "" || !filepath.IsAbs(root) {
		return "", invalid("canonical_root must be an absolute path")
	}
	normalized, err := workspacecatalog.NormalizeRelativePath(marker)
	if err != nil {
		return "", invalid("required_markers must be normalized relative files under the canonical root")
	}
	full := filepath.Join(root, filepath.FromSlash(normalized))
	relative, err := filepath.Rel(root, full)
	if err != nil || relative == "." || strings.HasPrefix(relative, "..") {
		return "", invalid("required_markers must stay under the canonical root")
	}
	return full, nil
}

func ProbeRequiredMarkers(canonicalRoot string, markers []string) ([]string, error) {
	if !CanProbeMarkers(canonicalRoot) {
		return nil, ErrRemoteMarkers
	}
	missing := make([]string, 0)
	for _, marker := range markers {
		path, err := MarkerPath(canonicalRoot, marker)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			missing = append(missing, marker)
		}
	}
	return missing, nil
}
