package rootfs

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func archive(t *testing.T, name, body string) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(tw, body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestInstallTarGz(t *testing.T) {
	root := t.TempDir()
	if err := InstallTarGz(bytes.NewReader(archive(t, "etc/motd", "welcome\n")), root, 1024); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "etc/motd"))
	if err != nil || string(data) != "welcome\n" {
		t.Fatalf("installed file=(%q,%v)", data, err)
	}
}

func TestInstallRejectsTraversal(t *testing.T) {
	if err := InstallTarGz(bytes.NewReader(archive(t, "../../outside", "bad")), t.TempDir(), 1024); err == nil {
		t.Fatal("expected unsafe tar entry to be rejected")
	}
}
