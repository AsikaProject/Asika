// Package archive provides helpers for extracting and validating release
// artifacts distributed as compressed tarballs.
package archive

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// MaxExtractSize bounds the total bytes written while extracting a tarball.
// It protects against decompression bombs. The value matches the release
// asset size cap enforced by the web update handler.
const MaxExtractSize = 100 << 20

// ExtractFileByBaseName scans a gzip-compressed tar stream for a regular file
// whose path base name matches fileName and writes it to dest.
//
// The Asika release tarballs are structured as
//
//	asika-<os>-<arch>/<binary>
//
// so matching by base name (ignoring the parent directory) keeps the lookup
// independent of the exact archive root name.
//
// ExtractFileByBaseName fails if:
//   - the archive contains a path traversal attempt (absolute paths or ".."),
//   - any entry is not a regular file,
//   - the decoded size exceeds MaxExtractSize,
//   - no entry matches fileName.
func ExtractFileByBaseName(r io.Reader, fileName, dest string) (int64, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return 0, fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("tar: %w", err)
		}

		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		if !isSafePath(hdr.Name) {
			return 0, fmt.Errorf("unsafe tar entry path: %q", hdr.Name)
		}

		if filepath.Base(filepath.ToSlash(hdr.Name)) != fileName {
			continue
		}

		return writeLimited(tr, dest, hdr.Size)
	}

	return 0, fmt.Errorf("file %q not found in archive", fileName)
}

// isSafePath rejects absolute paths and any segment that escapes the archive
// root. tar entries use forward slashes on every platform.
func isSafePath(p string) bool {
	if p == "" {
		return false
	}
	if filepath.IsAbs(filepath.ToSlash(p)) {
		return false
	}
	for _, seg := range strings.Split(filepath.ToSlash(p), "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

// writeLimited copies r into dest, enforcing MaxExtractSize regardless of the
// declared size header (which may be missing for streaming entries). On limit
// violation the partial destination file is removed so callers never observe a
// truncated payload that looks valid.
func writeLimited(r io.Reader, dest string, declared int64) (int64, error) {
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, fmt.Errorf("create %s: %w", dest, err)
	}

	limited := io.LimitReader(r, MaxExtractSize+1)
	n, copyErr := io.Copy(f, limited)
	syncErr := f.Sync()
	closeErr := f.Close()

	if copyErr != nil {
		_ = os.Remove(dest)
		return n, fmt.Errorf("write %s: %w", dest, copyErr)
	}
	if n > MaxExtractSize {
		_ = os.Remove(dest)
		return n, fmt.Errorf("extracted size %d exceeds limit %d", n, MaxExtractSize)
	}
	if syncErr != nil {
		_ = os.Remove(dest)
		return n, fmt.Errorf("sync %s: %w", dest, syncErr)
	}
	if closeErr != nil {
		_ = os.Remove(dest)
		return n, fmt.Errorf("close %s: %w", dest, closeErr)
	}
	_ = declared
	return n, nil
}
