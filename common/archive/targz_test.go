package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildTarball constructs an in-memory gzip-compressed tar archive from the
// provided entries. Type flag defaults to TypeReg; callers may override via
// the embedded tar.Header fields by passing a pointer.
func buildTarball(t *testing.T, entries ...struct {
	Name string
	Body []byte
	Flag byte
}) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for _, e := range entries {
		flag := e.Flag
		if flag == 0 {
			flag = tar.TypeReg
		}
		hdr := &tar.Header{
			Name:     e.Name,
			Typeflag: flag,
			Size:     int64(len(e.Body)),
			Mode:     0o644,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header %q: %v", e.Name, err)
		}
		if flag == tar.TypeReg {
			if _, err := tw.Write(e.Body); err != nil {
				t.Fatalf("write body %q: %v", e.Name, err)
			}
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	return buf.Bytes()
}

func TestExtractFileByBaseName_LinuxDaemon(t *testing.T) {
	dir := t.TempDir()
	data := []byte("fake asikad binary payload")
	tgz := buildTarball(t,
		struct {
			Name string
			Body []byte
			Flag byte
		}{Name: "asika-linux-arm64/asikad", Body: data},
		struct {
			Name string
			Body []byte
			Flag byte
		}{Name: "asika-linux-arm64/asika", Body: []byte("cli payload")},
		struct {
			Name string
			Body []byte
			Flag byte
		}{Name: "asika-linux-arm64/asikad.service", Body: []byte("systemd unit")},
	)

	dest := filepath.Join(dir, "asikad")
	n, err := ExtractFileByBaseName(bytes.NewReader(tgz), "asikad", dest)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if n != int64(len(data)) {
		t.Errorf("written bytes = %d, want %d", n, len(data))
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("extracted content mismatch: got %q, want %q", got, data)
	}
}

func TestExtractFileByBaseName_WindowsExe(t *testing.T) {
	dir := t.TempDir()
	data := []byte("windows exe payload")
	tgz := buildTarball(t,
		struct {
			Name string
			Body []byte
			Flag byte
		}{Name: "asika-windows-amd64/asikad.exe", Body: data},
		struct {
			Name string
			Body []byte
			Flag byte
		}{Name: "asika-windows-amd64/asika.exe", Body: []byte("cli")},
	)

	dest := filepath.Join(dir, "asikad.exe")
	if _, err := ExtractFileByBaseName(bytes.NewReader(tgz), "asikad.exe", dest); err != nil {
		t.Fatalf("extract: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("content mismatch: got %q, want %q", got, data)
	}
}

func TestExtractFileByBaseName_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	tgz := buildTarball(t,
		struct {
			Name string
			Body []byte
			Flag byte
		}{Name: "../../etc/passwd", Body: []byte("evil")},
		struct {
			Name string
			Body []byte
			Flag byte
		}{Name: "asika-linux-arm64/asikad", Body: []byte("legit")},
	)

	dest := filepath.Join(dir, "out")
	_, err := ExtractFileByBaseName(bytes.NewReader(tgz), "passwd", dest)
	if err == nil {
		t.Fatal("expected error for path traversal entry, got nil")
	}
	if !strings.Contains(err.Error(), "unsafe") {
		t.Errorf("error should mention unsafe path, got: %v", err)
	}

	// Ensure the malicious file was NOT written outside temp dir.
	if _, err := os.Stat(filepath.Join(dir, "..", "..", "etc", "passwd")); err == nil {
		t.Error("path traversal escaped the archive root")
	}
}

func TestExtractFileByBaseName_AbsolutePath(t *testing.T) {
	dir := t.TempDir()
	tgz := buildTarball(t,
		struct {
			Name string
			Body []byte
			Flag byte
		}{Name: "/etc/shadow", Body: []byte("evil")},
	)

	dest := filepath.Join(dir, "out")
	_, err := ExtractFileByBaseName(bytes.NewReader(tgz), "shadow", dest)
	if err == nil {
		t.Fatal("expected error for absolute path entry")
	}
}

func TestExtractFileByBaseName_SizeLimit(t *testing.T) {
	dir := t.TempDir()
	// Build an archive just over the limit.
	huge := bytes.Repeat([]byte("x"), MaxExtractSize+1024)
	tgz := buildTarball(t,
		struct {
			Name string
			Body []byte
			Flag byte
		}{Name: "asika-linux-arm64/asikad", Body: huge},
	)

	dest := filepath.Join(dir, "out")
	_, err := ExtractFileByBaseName(bytes.NewReader(tgz), "asikad", dest)
	if err == nil {
		t.Fatal("expected size-limit error, got nil")
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Errorf("error should mention size limit, got: %v", err)
	}
	// dest must not retain the oversized partial write beyond the cap.
	info, statErr := os.Stat(dest)
	if statErr == nil && info.Size() > MaxExtractSize {
		t.Errorf("extracted file exceeded cap: %d bytes", info.Size())
	}
}

func TestExtractFileByBaseName_CorruptGzip(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out")
	_, err := ExtractFileByBaseName(strings.NewReader("not a gzip stream"), "asikad", dest)
	if err == nil {
		t.Fatal("expected error for corrupt gzip")
	}
}

func TestExtractFileByBaseName_NotFound(t *testing.T) {
	dir := t.TempDir()
	tgz := buildTarball(t,
		struct {
			Name string
			Body []byte
			Flag byte
		}{Name: "asika-linux-arm64/asikad", Body: []byte("payload")},
	)

	dest := filepath.Join(dir, "out")
	_, err := ExtractFileByBaseName(bytes.NewReader(tgz), "nonexistent", dest)
	if err == nil {
		t.Fatal("expected error for missing entry")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention not found, got: %v", err)
	}
}

func TestExtractFileByBaseName_SkipsNonRegular(t *testing.T) {
	dir := t.TempDir()
	// Symlink entry with target base name matching the requested file should be ignored.
	tgz := buildTarball(t,
		struct {
			Name string
			Body []byte
			Flag byte
		}{Name: "asika-linux-arm64/asikad", Body: []byte("linktarget"), Flag: tar.TypeSymlink},
		struct {
			Name string
			Body []byte
			Flag byte
		}{Name: "asika-linux-arm64/asikad", Body: []byte("real")},
	)

	dest := filepath.Join(dir, "out")
	n, err := ExtractFileByBaseName(bytes.NewReader(tgz), "asikad", dest)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if n != 4 {
		t.Errorf("expected 4 bytes from real file, got %d", n)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "real" {
		t.Errorf("got %q, want %q", got, "real")
	}
}

func TestIsSafePath(t *testing.T) {
	cases := []struct {
		p    string
		want bool
	}{
		{"asika-linux-arm64/asikad", true},
		{"asikad", true},
		{"a/b/c/d", true},
		{"", false},
		{"/etc/passwd", false},
		{"../etc/passwd", false},
		{"a/../../b", false},
		{"a/../b", false},
		{"a/./b", true},
	}
	for _, c := range cases {
		if got := isSafePath(c.p); got != c.want {
			t.Errorf("isSafePath(%q) = %v, want %v", c.p, got, c.want)
		}
	}
}

// Ensure that gzip EOF handling is exercised for empty archives.
func TestExtractFileByBaseName_EmptyArchive(t *testing.T) {
	dir := t.TempDir()
	tgz := buildTarball(t)
	dest := filepath.Join(dir, "out")
	_, err := ExtractFileByBaseName(bytes.NewReader(tgz), "asikad", dest)
	if err == nil {
		t.Fatal("expected error for empty archive")
	}
	_, _ = io.Discard.Write(nil) // keep io import used even if future tests drop it
}
