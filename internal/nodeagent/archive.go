package nodeagent

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/XR-Lee/Gemcp/internal/sourcearchive"
)

const maxArchiveEntries = 100000

type archiveSymlink struct {
	name string
	link string
}

func extractSourceArchive(filename, destination string, maximum int64) error {
	if maximum <= 0 {
		return fmt.Errorf("source extraction limit must be positive")
	}
	if err := inspectSourceArchive(filename, maximum); err != nil {
		return err
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return fmt.Errorf("create source extraction directory: %w", err)
	}
	reader, closeReader, err := openTarGzip(filename)
	if err != nil {
		return err
	}
	defer closeReader()
	var symlinks []archiveSymlink
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read source archive: %w", err)
		}
		target := filepath.Join(destination, filepath.FromSlash(path.Clean(header.Name)))
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)&0o777); err != nil {
				return fmt.Errorf("create archived directory: %w", err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			mode := os.FileMode(header.Mode) & 0o777
			file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if err != nil {
				return fmt.Errorf("create archived file: %w", err)
			}
			_, copyErr := io.CopyN(file, reader, header.Size)
			closeErr := file.Close()
			if copyErr != nil {
				return fmt.Errorf("extract archived file: %w", copyErr)
			}
			if closeErr != nil {
				return closeErr
			}
		case tar.TypeSymlink:
			symlinks = append(symlinks, archiveSymlink{name: target, link: header.Linkname})
		case tar.TypeXGlobalHeader:
		}
	}
	for _, link := range symlinks {
		if err := os.MkdirAll(filepath.Dir(link.name), 0o700); err != nil {
			return err
		}
		if err := os.Symlink(link.link, link.name); err != nil {
			return fmt.Errorf("create archived symlink: %w", err)
		}
	}
	return nil
}

func inspectSourceArchive(filename string, maximum int64) error {
	reader, closeReader, err := openTarGzip(filename)
	if err != nil {
		return err
	}
	defer closeReader()
	var total int64
	for entries := 0; ; entries++ {
		if entries >= maxArchiveEntries {
			return fmt.Errorf("source archive has too many entries")
		}
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect source archive: %w", err)
		}
		cleanName := path.Clean(header.Name)
		if cleanName == "." || path.IsAbs(header.Name) || cleanName == ".." || strings.HasPrefix(cleanName, "../") || strings.ContainsRune(header.Name, 0) {
			return fmt.Errorf("source archive contains an unsafe path")
		}
		switch header.Typeflag {
		case tar.TypeDir:
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || total > maximum-header.Size {
				return fmt.Errorf("source archive exceeds its extraction limit")
			}
			total += header.Size
		case tar.TypeSymlink:
			linkTarget := path.Clean(path.Join(path.Dir(cleanName), header.Linkname))
			if path.IsAbs(header.Linkname) || linkTarget == ".." || strings.HasPrefix(linkTarget, "../") || strings.ContainsRune(header.Linkname, 0) {
				return fmt.Errorf("source archive contains an unsafe symlink")
			}
		case tar.TypeXGlobalHeader:
			if !sourcearchive.ValidGitGlobalHeader(header) {
				return fmt.Errorf("source archive contains unsafe global metadata")
			}
		default:
			return fmt.Errorf("source archive contains an unsupported entry")
		}
	}
}

func openTarGzip(filename string) (*tar.Reader, func(), error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, func() {}, fmt.Errorf("open source archive: %w", err)
	}
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		_ = file.Close()
		return nil, func() {}, fmt.Errorf("open compressed source archive: %w", err)
	}
	gzipReader.Multistream(false)
	closeReader := func() {
		_ = gzipReader.Close()
		_ = file.Close()
	}
	return tar.NewReader(gzipReader), closeReader, nil
}
