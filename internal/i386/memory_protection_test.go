package i386

import "testing"

func TestProtectedMemoryEnforcesMappingPermissions(t *testing.T) {
	space := NewAddressSpace(0x10000)
	if err := space.Add(0x1000, 0x1000, ProtRead|ProtExec, "code"); err != nil {
		t.Fatal(err)
	}
	if err := space.Add(0x3000, 0x1000, ProtRead|ProtWrite, "data"); err != nil {
		t.Fatal(err)
	}
	if err := space.Mem.WriteRaw(0x1000, []byte{0xf4}); err != nil {
		t.Fatal(err)
	}
	if err := space.Mem.WriteRaw(0x3000, []byte{7}); err != nil {
		t.Fatal(err)
	}
	space.EnableProtection()

	if got, err := space.Mem.Read8(0x1000); err != nil || got != 0xf4 {
		t.Fatalf("code read=(0x%x,%v)", got, err)
	}
	if _, err := space.Mem.Fetch8(0x1000); err != nil {
		t.Fatalf("executable fetch failed: %v", err)
	}
	if err := space.Mem.Write8(0x1000, 0x90); err == nil {
		t.Fatal("write to read/exec mapping unexpectedly succeeded")
	}
	if got, err := space.Mem.Read8(0x3000); err != nil || got != 7 {
		t.Fatalf("data read=(%d,%v)", got, err)
	}
	if err := space.Mem.Write8(0x3000, 8); err != nil {
		t.Fatalf("data write failed: %v", err)
	}
	if _, err := space.Mem.Fetch8(0x3000); err == nil {
		t.Fatal("fetch from non-executable mapping unexpectedly succeeded")
	}
	if _, err := space.Mem.Read8(0x2000); err == nil {
		t.Fatal("read from unmapped page unexpectedly succeeded")
	}
	if _, err := space.Mem.ReadBytes(0x1fff, 2); err == nil {
		t.Fatal("crossing into unmapped page unexpectedly succeeded")
	}
}

func TestProtectedMemoryCanChangePermissions(t *testing.T) {
	space := NewAddressSpace(0x10000)
	if err := space.Add(0x5000, 0x1000, ProtRead|ProtExec, "code"); err != nil {
		t.Fatal(err)
	}
	if err := space.Mem.WriteRaw(0x5000, []byte{0x90}); err != nil {
		t.Fatal(err)
	}
	space.EnableProtection()
	if err := space.Protect(0x5000, 0x1000, ProtRead|ProtWrite); err != nil {
		t.Fatal(err)
	}
	if err := space.Mem.Write8(0x5000, 0xcc); err != nil {
		t.Fatalf("write after mprotect failed: %v", err)
	}
	if _, err := space.Mem.Fetch8(0x5000); err == nil {
		t.Fatal("fetch after removing execute permission unexpectedly succeeded")
	}
	if err := space.Protect(0x5000, 0x1000, ProtRead|ProtExec); err != nil {
		t.Fatal(err)
	}
	if _, err := space.Mem.Fetch8(0x5000); err != nil {
		t.Fatalf("fetch after restoring execute failed: %v", err)
	}
}

func TestCPUFetchUsesExecutePermission(t *testing.T) {
	space := NewAddressSpace(0x10000)
	if err := space.Add(0x6000, 0x1000, ProtRead|ProtWrite, "not-code"); err != nil {
		t.Fatal(err)
	}
	if err := space.Mem.WriteRaw(0x6000, []byte{0x90}); err != nil {
		t.Fatal(err)
	}
	space.EnableProtection()
	cpu := NewCPU(space.Mem)
	cpu.EIP = 0x6000
	if err := cpu.Step(); err == nil {
		t.Fatal("CPU executed non-executable mapping")
	}
}
