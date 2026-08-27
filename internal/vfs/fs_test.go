package vfs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootIsolationAndFiles(t *testing.T) {
	root := t.TempDir()
	f, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.WriteFile("/etc/motd", []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := f.ReadFile("/etc/motd")
	if err != nil || string(got) != "hello\n" {
		t.Fatalf("ReadFile = (%q, %v)", got, err)
	}
	if _, err := f.Resolve("../../outside"); err == nil {
		t.Fatal("expected path traversal to be rejected")
	}
}

func TestProcMount(t *testing.T) {
	f, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f.MountProc("Linux ish-go", 7)
	data, err := f.ReadFile("/proc/self/status")
	if err != nil || !strings.Contains(string(data), "Pid:\t7") {
		t.Fatalf("proc status = (%q, %v)", data, err)
	}
	entries, err := f.List("/proc")
	if err != nil || len(entries) == 0 {
		t.Fatalf("proc entries = (%v, %v)", entries, err)
	}
}

func TestVirtualFileThroughGuestSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../proc/mounts", filepath.Join(root, "etc", "mtab")); err != nil {
		t.Fatal(err)
	}
	f, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	f.MountProc("Linux ish-go", 1)
	data, virtual, err := f.ReadVirtual("/etc/mtab")
	if err != nil || !virtual || !strings.Contains(string(data), "rootfs / rootfs") {
		t.Fatalf("ReadVirtual(/etc/mtab)=(%q,%t,%v)", data, virtual, err)
	}
	stat, err := f.Stat("/etc/mtab")
	if err != nil || stat.Size() != int64(len(data)) {
		t.Fatalf("Stat(/etc/mtab)=(%v,%v), size=%d", stat, err, len(data))
	}
}
