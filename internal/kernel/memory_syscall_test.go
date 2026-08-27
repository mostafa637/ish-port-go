package kernel

import (
	"testing"

	"example.com/ish-go/internal/i386"
	"example.com/ish-go/internal/pty"
	"example.com/ish-go/internal/vfs"
)

func TestMmapMprotectMunmap(t *testing.T) {
	mem := i386.NewMemory(1 << 20)
	cpu := i386.NewCPU(mem)
	space := i386.AddressSpaceFromMemory(mem)
	fsys, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	k := New(fsys, pty.New())
	k.SetAddressSpace(space)

	cpu.Regs[i386.EAX] = SysMmap2
	cpu.Regs[i386.EBX] = 0
	cpu.Regs[i386.ECX] = 0x2000
	cpu.Regs[i386.EDX] = 3
	cpu.Regs[i386.ESI] = 0x20 // MAP_ANONYMOUS
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	addr := cpu.Regs[i386.EAX]
	if addr == 0 || addr+0x2000 > mem.Size() {
		t.Fatalf("mmap returned 0x%x", addr)
	}
	if _, ok := space.MappingAt(addr); !ok {
		t.Fatal("mmap did not create mapping")
	}

	cpu.Regs[i386.EAX] = SysMprotect
	cpu.Regs[i386.EBX] = addr
	cpu.Regs[i386.ECX] = 0x1000
	cpu.Regs[i386.EDX] = 1
	if err := k.Handle(cpu); err != nil || cpu.Regs[i386.EAX] != 0 {
		t.Fatalf("mprotect=(%d,%v)", cpu.Regs[i386.EAX], err)
	}
	m, ok := space.MappingAt(addr)
	if !ok || m.Prot != i386.ProtRead {
		t.Fatalf("mprotect mapping=(%+v,%t)", m, ok)
	}

	cpu.Regs[i386.EAX] = SysMunmap
	cpu.Regs[i386.EBX] = addr
	cpu.Regs[i386.ECX] = 0x2000
	if err := k.Handle(cpu); err != nil || cpu.Regs[i386.EAX] != 0 {
		t.Fatalf("munmap=(%d,%v)", cpu.Regs[i386.EAX], err)
	}
	if _, ok := space.MappingAt(addr); ok {
		t.Fatal("munmap left mapping behind")
	}
}

func TestMmapFlagsRespectHintAndFixedRange(t *testing.T) {
	mem := i386.NewMemory(1 << 20)
	cpu := i386.NewCPU(mem)
	space := i386.AddressSpaceFromMemory(mem)
	fsys, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	k := New(fsys, pty.New())
	k.SetAddressSpace(space)

	cpu.Regs[i386.EAX] = SysMmap2
	cpu.Regs[i386.EBX] = 0x90000000
	cpu.Regs[i386.ECX] = 0x1000
	cpu.Regs[i386.EDX] = 3
	cpu.Regs[i386.ESI] = 0x20 // MAP_ANONYMOUS
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if addr := cpu.Regs[i386.EAX]; addr == 0 || addr+0x1000 > mem.Size() {
		t.Fatalf("non-fixed hint escaped guest: 0x%x", addr)
	}

	cpu.Regs[i386.EAX] = SysMmap2
	cpu.Regs[i386.EBX] = 0x90000000
	cpu.Regs[i386.ECX] = 0x1000
	cpu.Regs[i386.EDX] = 3
	cpu.Regs[i386.ESI] = 0x30 // MAP_FIXED|MAP_ANONYMOUS
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != -ErrnoNoMem {
		t.Fatalf("fixed out-of-range mmap=%d want=%d", got, -ErrnoNoMem)
	}
}

func TestMmapFileBackedAndAnonymousZeroFill(t *testing.T) {
	root := t.TempDir()
	fsys, err := vfs.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := fsys.WriteFile("/data", []byte("mapped-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	mem := i386.NewMemory(1 << 20)
	space := i386.AddressSpaceFromMemory(mem)
	k := New(fsys, pty.New())
	k.SetAddressSpace(space)
	if err := mem.WriteBytes(100, []byte("/data\x00")); err != nil {
		t.Fatal(err)
	}
	cpu := i386.NewCPU(mem)
	cpu.Regs[i386.EAX] = SysOpen
	cpu.Regs[i386.EBX] = 100
	cpu.Regs[i386.ECX] = 0
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	fd := int32(cpu.Regs[i386.EAX])
	if fd < 3 {
		t.Fatalf("open fd=%d", fd)
	}
	cpu.Regs[i386.EAX] = SysMmap2
	cpu.Regs[i386.EBX] = 0
	cpu.Regs[i386.ECX] = 0x1000
	cpu.Regs[i386.EDX] = 1
	cpu.Regs[i386.ESI] = 2 // MAP_PRIVATE
	cpu.Regs[i386.EDI] = uint32(fd)
	cpu.Regs[i386.EBP] = 0
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	mapped := cpu.Regs[i386.EAX]
	data, err := mem.ReadBytes(mapped, 11)
	if err != nil || string(data) != "mapped-data" {
		t.Fatalf("file mapping=(%q,%v)", data, err)
	}
	cpu.Regs[i386.EAX] = SysMmap2
	cpu.Regs[i386.EBX] = 0
	cpu.Regs[i386.ECX] = 0x1000
	cpu.Regs[i386.EDX] = 3
	cpu.Regs[i386.ESI] = 0x22 // MAP_PRIVATE|MAP_ANONYMOUS
	cpu.Regs[i386.EDI] = ^uint32(0)
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	anon := cpu.Regs[i386.EAX]
	if err := mem.Write32(anon, 0xdeadbeef); err != nil {
		t.Fatal(err)
	}
	if err := space.Remove(anon, 0x1000); err != nil {
		t.Fatal(err)
	}
	cpu.Regs[i386.EAX] = SysMmap2
	cpu.Regs[i386.EBX] = anon
	cpu.Regs[i386.ECX] = 0x1000
	cpu.Regs[i386.EDX] = 3
	cpu.Regs[i386.ESI] = 0x32 // MAP_FIXED|MAP_PRIVATE|MAP_ANONYMOUS
	cpu.Regs[i386.EDI] = ^uint32(0)
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	zero, err := mem.Read32(anon)
	if err != nil || zero != 0 {
		t.Fatalf("anonymous mapping not zero-filled: 0x%x,%v", zero, err)
	}
}

func TestMmapFixedFailurePreservesPreviousMapping(t *testing.T) {
	mem := i386.NewMemory(1 << 20)
	cpu := i386.NewCPU(mem)
	space := i386.AddressSpaceFromMemory(mem)
	fsys, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	k := New(fsys, pty.New())
	k.SetAddressSpace(space)

	cpu.Regs[i386.EAX] = SysMmap2
	cpu.Regs[i386.EBX] = 0x3000
	cpu.Regs[i386.ECX] = 0x1000
	cpu.Regs[i386.EDX] = 3
	cpu.Regs[i386.ESI] = 0x30 // MAP_FIXED|MAP_ANONYMOUS
	cpu.Regs[i386.EDI] = ^uint32(0)
	if err := k.Handle(cpu); err != nil || cpu.Regs[i386.EAX] != 0x3000 {
		t.Fatalf("initial fixed mmap=(0x%x,%v)", cpu.Regs[i386.EAX], err)
	}
	if err := mem.Write32(0x3000, 0xdeadbeef); err != nil {
		t.Fatal(err)
	}

	cpu.Regs[i386.EAX] = SysMmap2
	cpu.Regs[i386.EBX] = 0x3000
	cpu.Regs[i386.ECX] = 0x1000
	cpu.Regs[i386.EDX] = 1
	cpu.Regs[i386.ESI] = 0x10 // MAP_FIXED, file-backed
	cpu.Regs[i386.EDI] = 99
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != -ErrnoBadFD {
		t.Fatalf("failed fixed mmap=%d want=%d", got, -ErrnoBadFD)
	}
	if _, ok := space.MappingAt(0x3000); !ok {
		t.Fatal("failed MAP_FIXED removed previous mapping")
	}
	if got, err := mem.Read32(0x3000); err != nil || got != 0xdeadbeef {
		t.Fatalf("failed MAP_FIXED changed data=0x%x err=%v", got, err)
	}
}

func TestMremapGrowShrinkAndMove(t *testing.T) {
	mem := i386.NewMemory(1 << 20)
	cpu := i386.NewCPU(mem)
	space := i386.AddressSpaceFromMemory(mem)
	fsys, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	k := New(fsys, pty.New())
	k.SetAddressSpace(space)
	if _, err := space.MapAnonymous(0x20000, 0x1000, i386.ProtRead|i386.ProtWrite, "mremap"); err != nil {
		t.Fatal(err)
	}
	if err := mem.Write32(0x20000, 0x12345678); err != nil {
		t.Fatal(err)
	}

	cpu.Regs[i386.EAX] = SysMremap
	cpu.Regs[i386.EBX] = 0x20000
	cpu.Regs[i386.ECX] = 0x1000
	cpu.Regs[i386.EDX] = 0x2000
	cpu.Regs[i386.ESI] = 0
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := cpu.Regs[i386.EAX]; got != 0x20000 {
		t.Fatalf("in-place grow=0x%x want=0x20000", got)
	}
	if got, err := mem.Read32(0x20000); err != nil || got != 0x12345678 {
		t.Fatalf("grow data=0x%x err=%v", got, err)
	}
	if got, err := mem.Read32(0x21000); err != nil || got != 0 {
		t.Fatalf("grow zero-fill=0x%x err=%v", got, err)
	}
	if _, err := space.MapAnonymous(0x22000, 0x1000, i386.ProtRead, "block"); err != nil {
		t.Fatal(err)
	}

	cpu.Regs[i386.EAX] = SysMremap
	cpu.Regs[i386.EBX] = 0x20000
	cpu.Regs[i386.ECX] = 0x2000
	cpu.Regs[i386.EDX] = 0x1000
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := cpu.Regs[i386.EAX]; got != 0x20000 {
		t.Fatalf("shrink=0x%x want=0x20000", got)
	}

	cpu.Regs[i386.EAX] = SysMremap
	cpu.Regs[i386.EBX] = 0x20000
	cpu.Regs[i386.ECX] = 0x1000
	cpu.Regs[i386.EDX] = 0x3000
	cpu.Regs[i386.ESI] = 1 // MREMAP_MAYMOVE
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	moved := cpu.Regs[i386.EAX]
	if moved == 0x20000 || moved == ^uint32(0) {
		t.Fatalf("move result=0x%x", moved)
	}
	if got, err := mem.Read32(moved); err != nil || got != 0x12345678 {
		t.Fatalf("moved data=0x%x err=%v", got, err)
	}
	if _, ok := space.MappingAt(0x20000); ok {
		t.Fatal("old mapping remained after move")
	}
}

func TestMunmapAndMprotectRequirePageAlignedAddress(t *testing.T) {
	mem := i386.NewMemory(1 << 20)
	cpu := i386.NewCPU(mem)
	space := i386.AddressSpaceFromMemory(mem)
	fsys, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	k := New(fsys, pty.New())
	k.SetAddressSpace(space)
	if _, err := space.MapAnonymous(0x30000, 0x1000, i386.ProtRead|i386.ProtWrite, "page"); err != nil {
		t.Fatal(err)
	}

	cpu.Regs[i386.EAX] = SysMunmap
	cpu.Regs[i386.EBX] = 0x30001
	cpu.Regs[i386.ECX] = 0x1000
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != -ErrnoInvalid {
		t.Fatalf("unaligned munmap=%d want=%d", got, -ErrnoInvalid)
	}
	if _, ok := space.MappingAt(0x30000); !ok {
		t.Fatal("unaligned munmap removed mapping")
	}

	cpu.Regs[i386.EAX] = SysMprotect
	cpu.Regs[i386.EBX] = 0x30001
	cpu.Regs[i386.ECX] = 0x1000
	cpu.Regs[i386.EDX] = 1
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != -ErrnoInvalid {
		t.Fatalf("unaligned mprotect=%d want=%d", got, -ErrnoInvalid)
	}
}
