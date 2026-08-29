package datasetcatalog

import "testing"

func TestNormalizeSourceAcceptsAllowlistedHTTPS(t *testing.T) {
	source, err := NormalizeSource(SourceFile{
		URL:          "https://huggingface.co/datasets/example/resolve/main/train.h5",
		RelativePath: "main_split/train.h5",
		SHA256:       "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	if err != nil || source.RelativePath != "main_split/train.h5" || source.SHA256 == "" {
		t.Fatalf("source=%+v err=%v", source, err)
	}
}

func TestNormalizeSourceRejectsUnsafeURLs(t *testing.T) {
	for _, url := range []string{
		"http://huggingface.co/datasets/example/file.h5",
		"https://evil.example/file.h5",
		"https://user:pass@huggingface.co/file.h5",
		"https://huggingface.co/file.h5?token=1",
	} {
		if _, err := NormalizeSource(SourceFile{URL: url, RelativePath: "file.h5"}); err == nil {
			t.Fatalf("accepted %q", url)
		}
	}
}

func TestLookupCatalogScanObjectNN(t *testing.T) {
	entry, ok := LookupCatalog("scanobjectnn-objbg")
	if !ok || entry.CanonicalRoot != "/root/autodl-fs/datasets/ScanObjectNN" {
		t.Fatalf("catalog=%+v ok=%v", entry, ok)
	}
}

func TestLookupCatalogModelNet40Mini(t *testing.T) {
	entry, ok := LookupCatalog("modelnet40-mini")
	if !ok || entry.Backend != BackendSSHCloud || entry.CanonicalRoot != "/opt/gemcp/datasets/modelnet40-mini" {
		t.Fatalf("catalog=%+v ok=%v", entry, ok)
	}
}

func TestNormalizeCanonicalRootForSSHCloud(t *testing.T) {
	root, err := NormalizeCanonicalRootForBackend("/root/autodl-fs/datasets/ScanObjectNN", BackendSSHCloud)
	if err != nil || root != "/root/autodl-fs/datasets/ScanObjectNN" {
		t.Fatalf("root=%q err=%v", root, err)
	}
	if _, err := NormalizeCanonicalRootForBackend("/etc/shadow", BackendSSHCloud); err == nil {
		t.Fatal("expected /etc/shadow to fail")
	}
}
