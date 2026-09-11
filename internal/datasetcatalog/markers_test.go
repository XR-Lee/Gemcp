package datasetcatalog

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCanProbeMarkersRejectsAutoDLRoots(t *testing.T) {
	if CanProbeMarkers("/root/autodl-fs/datasets/ScanObjectNN") {
		t.Fatal("probed AutoDL shared storage")
	}
	if !CanProbeMarkers("/home/ubuntu/gemcp/datasets/modelnet40-mini") {
		t.Fatal("rejected local host root")
	}
}

func TestProbeRequiredMarkersReportsMissingFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "meta.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing, err := ProbeRequiredMarkers(root, []string{"meta.json", "train/chair/0001.off"})
	if err != nil || len(missing) != 1 || missing[0] != "train/chair/0001.off" {
		t.Fatalf("missing=%v err=%v", missing, err)
	}
}

func TestProbeRequiredMarkersRejectsEscape(t *testing.T) {
	if _, err := ProbeRequiredMarkers(t.TempDir(), []string{"../secret"}); err == nil {
		t.Fatal("accepted escaped marker")
	}
}

func TestProbeRequiredMarkersDoesNotBrowseAutoDL(t *testing.T) {
	if _, err := ProbeRequiredMarkers("/root/autodl-fs/datasets/ScanObjectNN", []string{"train.h5"}); !errors.Is(err, ErrRemoteMarkers) {
		t.Fatalf("err=%v", err)
	}
}
