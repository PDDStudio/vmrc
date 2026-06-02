package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// errBinaryNotInArchive means the archive downloaded fine but did not contain
// the expected binary entry (skipping README/LICENSE).
var errBinaryNotInArchive = errors.New("binary not found in archive")

// maxBinarySize caps a single extracted file (defense against a malicious or
// corrupt archive). The current agent binary is ~42 MB.
const maxBinarySize = 200 << 20 // 200 MiB

// extractBinary pulls the file named binaryName (or binaryName+".exe") out of a
// goreleaser archive, skipping README/LICENSE. The format is chosen from
// archiveName's extension (.zip vs .tar.gz).
func extractBinary(archive []byte, archiveName, binaryName string) ([]byte, error) {
	if strings.HasSuffix(archiveName, ".zip") {
		return extractFromZip(archive, binaryName)
	}
	return extractFromTarGz(archive, binaryName)
}

// wantName matches an archive entry's base name against the binary name.
func wantName(name, binaryName string) bool {
	base := path.Base(name)
	return base == binaryName || base == binaryName+".exe"
}

func extractFromTarGz(archive []byte, binaryName string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || !wantName(hdr.Name, binaryName) {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(tr, maxBinarySize))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", hdr.Name, err)
		}
		return b, nil
	}
	return nil, fmt.Errorf("%w: %q", errBinaryNotInArchive, binaryName)
}

func extractFromZip(archive []byte, binaryName string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("zip: %w", err)
	}
	for _, f := range zr.File {
		// Reject path-traversal entries defensively even though we only read bytes.
		if strings.Contains(f.Name, "..") {
			continue
		}
		if f.FileInfo().IsDir() || !wantName(f.Name, binaryName) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("opening %s: %w", f.Name, err)
		}
		b, err := io.ReadAll(io.LimitReader(rc, maxBinarySize))
		_ = rc.Close()
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", f.Name, err)
		}
		return b, nil
	}
	return nil, fmt.Errorf("%w: %q", errBinaryNotInArchive, binaryName)
}
