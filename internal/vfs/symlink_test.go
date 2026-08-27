package vfs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("no"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	fsys, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fsys.Resolve("/link/secret"); err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
}

func TestResolveGuestAbsoluteSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "busybox"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/bin/busybox", filepath.Join(root, "bin", "echo")); err != nil {
		t.Fatal(err)
	}
	fsys, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := fsys.Resolve("/bin/echo")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "bin", "busybox")
	if resolved != want {
		t.Fatalf("resolved=%q want=%q", resolved, want)
	}
}
