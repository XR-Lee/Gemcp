package datasetcatalog

import "testing"

func TestNormalizeCanonicalRootAcceptsAutoDLDatasetPath(t *testing.T) {
	root, err := NormalizeCanonicalRoot("/root/autodl-fs/datasets/ScanObjectNN")
	if err != nil || root != "/root/autodl-fs/datasets/ScanObjectNN" {
		t.Fatalf("root=%q err=%v", root, err)
	}
}

func TestNormalizeCanonicalRootRejectsBroadOrForeignPaths(t *testing.T) {
	for _, value := range []string{
		"/root/autodl-fs",
		"/root/autodl-fs/",
		"/root/autodl-tmp/datasets/ScanObjectNN",
		"/gemcp/workspace/data",
		"/root/autodl-fs/datasets/../secret",
		"datasets/ScanObjectNN",
	} {
		if _, err := NormalizeCanonicalRoot(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}

func TestNormalizeMarkersDeduplicatesRelativeFiles(t *testing.T) {
	markers, err := NormalizeMarkers([]string{"main_split/train.h5", "main_split/train.h5"})
	if err != nil || len(markers) != 1 || markers[0] != "main_split/train.h5" {
		t.Fatalf("markers=%v err=%v", markers, err)
	}
}
