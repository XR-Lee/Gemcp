package sourcearchive

import (
	"archive/tar"
	"compress/gzip"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"strings"

	gitrepository "github.com/XR-Lee/Gemcp/internal/repository"
)

const maxEntries = 100000

type Inspection struct {
	Entries         int
	CompressedBytes int64
	PayloadBytes    int64
}

const maxRootFileBytes = 64 << 10

// ReadRootFile returns the contents of name at the archive root, or one directory deep
// as emitted by `git archive --prefix=repo/`. Nested copies are ignored.
func ReadRootFile(archive gitrepository.Archive, name string, maximum int64) ([]byte, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "/") || !safePath(name) {
		return nil, fmt.Errorf("root file name is invalid")
	}
	if archive == nil {
		return nil, fmt.Errorf("source archive is unavailable")
	}
	if archive.SizeBytes() < 1 || archive.SizeBytes() > maximum {
		return nil, fmt.Errorf("compressed source archive size is outside the configured bound")
	}
	reader, err := archive.Open()
	if err != nil {
		return nil, fmt.Errorf("open source archive: %w", err)
	}
	defer reader.Close()
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return nil, fmt.Errorf("open source gzip stream: %w", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	var found []byte
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read source tar stream: %w", err)
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			continue
		}
		if !rootFileName(header.Name, name) {
			continue
		}
		if header.Size < 1 || header.Size > maxRootFileBytes {
			return nil, fmt.Errorf("%s exceeds the configured bound", name)
		}
		payload, err := io.ReadAll(io.LimitReader(tarReader, header.Size+1))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		if int64(len(payload)) != header.Size {
			return nil, fmt.Errorf("%s size did not match the archive header", name)
		}
		if found != nil {
			return nil, fmt.Errorf("source archive contains more than one %s at the root", name)
		}
		found = payload
	}
	if found == nil {
		return nil, fmt.Errorf("%s was not found at the source root", name)
	}
	return found, nil
}

func rootFileName(entry, want string) bool {
	cleaned := path.Clean(strings.TrimPrefix(entry, "./"))
	if cleaned == want {
		return true
	}
	dir, base := path.Split(cleaned)
	dir = strings.TrimSuffix(dir, "/")
	return base == want && dir != "" && !strings.Contains(dir, "/")
}

func Inspect(archive gitrepository.Archive, maximum int64) (Inspection, error) {
	var result Inspection
	if archive == nil {
		return result, fmt.Errorf("source archive is unavailable")
	}
	result.CompressedBytes = archive.SizeBytes()
	if result.CompressedBytes < 1 || result.CompressedBytes > maximum {
		return result, fmt.Errorf("compressed source archive size is outside the configured bound")
	}
	reader, err := archive.Open()
	if err != nil {
		return result, fmt.Errorf("open source archive: %w", err)
	}
	defer reader.Close()
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return result, fmt.Errorf("open source gzip stream: %w", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return result, fmt.Errorf("read source tar stream: %w", err)
		}
		result.Entries++
		if result.Entries > maxEntries {
			return result, fmt.Errorf("source archive has too many entries")
		}
		if !safePath(header.Name) {
			return result, fmt.Errorf("source archive contains unsafe path %q", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || result.PayloadBytes > maximum-header.Size {
				return result, fmt.Errorf("source archive payload exceeds the configured bound")
			}
			result.PayloadBytes += header.Size
		case tar.TypeDir:
		case tar.TypeSymlink:
			if !safeLinkTarget(header.Name, header.Linkname) {
				return result, fmt.Errorf("source archive contains unsafe symlink")
			}
		case tar.TypeLink:
			if !safePath(header.Linkname) {
				return result, fmt.Errorf("source archive contains unsafe hard link")
			}
		case tar.TypeXGlobalHeader:
			if !ValidGitGlobalHeader(header) {
				return result, fmt.Errorf("source archive contains unsafe global metadata")
			}
		default:
			return result, fmt.Errorf("source archive contains unsupported entry type")
		}
	}
	if result.Entries == 0 {
		return result, fmt.Errorf("source archive is empty")
	}
	return result, nil
}

// ValidGitGlobalHeader accepts only the commit marker emitted by git archive.
func ValidGitGlobalHeader(header *tar.Header) bool {
	if header == nil || header.Name != "pax_global_header" || len(header.PAXRecords) != 1 {
		return false
	}
	commitSHA, ok := header.PAXRecords["comment"]
	if !ok || (len(commitSHA) != 40 && len(commitSHA) != 64) {
		return false
	}
	_, err := hex.DecodeString(commitSHA)
	return err == nil
}

func safePath(value string) bool {
	if value == "" || strings.HasPrefix(value, "/") {
		return false
	}
	cleaned := path.Clean(value)
	return cleaned != "." && cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}

func safeLinkTarget(name, target string) bool {
	if target == "" || strings.HasPrefix(target, "/") {
		return false
	}
	resolved := path.Clean(path.Join(path.Dir(name), target))
	return resolved != ".." && !strings.HasPrefix(resolved, "../")
}
