package diagnostic

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"path"
	"strings"

	gitrepository "github.com/XR-Lee/Gemcp/internal/repository"
)

const maxArchiveEntries = 100000

type archiveInspection struct {
	Entries         int
	CompressedBytes int64
	PayloadBytes    int64
}

func inspectArchive(archive gitrepository.Archive, maximum int64) (archiveInspection, error) {
	var result archiveInspection
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
		if result.Entries > maxArchiveEntries {
			return result, fmt.Errorf("source archive has too many entries")
		}
		if !safeArchivePath(header.Name) {
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
			if !safeArchivePath(header.Linkname) {
				return result, fmt.Errorf("source archive contains unsafe hard link")
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

func safeArchivePath(value string) bool {
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
