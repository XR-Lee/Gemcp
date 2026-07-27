package sourcearchive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"
)

type memoryArchive struct{ data []byte }

func (a memoryArchive) Open() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(a.data)), nil
}
func (a memoryArchive) Close() error     { return nil }
func (a memoryArchive) SizeBytes() int64 { return int64(len(a.data)) }

func TestInspectAcceptsGitGlobalCommitHeader(t *testing.T) {
	archive := gitStyleArchive(t, map[string]string{"comment": strings.Repeat("a", 40)})
	inspection, err := Inspect(memoryArchive{data: archive}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Entries != 2 || inspection.PayloadBytes != 2 {
		t.Fatalf("inspection = %+v", inspection)
	}
}

func TestInspectRejectsUntrustedGlobalMetadata(t *testing.T) {
	for name, records := range map[string]map[string]string{
		"non-hex commit": {"comment": strings.Repeat("z", 40)},
		"extra field":    {"comment": strings.Repeat("a", 40), "path": "elsewhere"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Inspect(memoryArchive{data: gitStyleArchive(t, records)}, 1<<20); err == nil || !strings.Contains(err.Error(), "unsafe global metadata") {
				t.Fatalf("Inspect() error = %v", err)
			}
		})
	}
}

func gitStyleArchive(t *testing.T, records map[string]string) []byte {
	t.Helper()
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "pax_global_header", Typeflag: tar.TypeXGlobalHeader, PAXRecords: records}); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.WriteHeader(&tar.Header{Name: "ok.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write([]byte("ok")); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
