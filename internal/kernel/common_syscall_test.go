package kernel

import (
	"testing"

	"example.com/ish-go/internal/i386"
	"example.com/ish-go/internal/pty"
	"example.com/ish-go/internal/vfs"
)

func TestCommonFileAndRandomSyscalls(t *testing.T) {
	fsys, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mem := i386.NewMemory(16 << 10)
	cpu := i386.NewCPU(mem)
	k := New(fsys, pty.New())
	if err := mem.WriteBytes(100, []byte("/dir\x00")); err != nil {
		t.Fatal(err)
	}
	cpu.Regs[i386.EAX] = SysMkdir
	cpu.Regs[i386.EBX] = 100
	cpu.Regs[i386.ECX] = 0o755
	if err := k.Handle(cpu); err != nil || cpu.Regs[i386.EAX] != 0 {
		t.Fatalf("mkdir=(%d,%v)", cpu.Regs[i386.EAX], err)
	}
	if err := mem.WriteBytes(100, []byte("/dir/a\x00")); err != nil {
		t.Fatal(err)
	}
	cpu.Regs[i386.EAX] = SysOpen
	cpu.Regs[i386.EBX] = 100
	cpu.Regs[i386.ECX] = 0x41 // O_WRONLY|O_CREAT
	cpu.Regs[i386.EDX] = 0o600
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	fd := int(cpu.Regs[i386.EAX])
	if fd < 3 {
		t.Fatalf("open fd=%d", fd)
	}
	if err := mem.WriteBytes(200, []byte("abcdef")); err != nil {
		t.Fatal(err)
	}
	cpu.Regs[i386.EAX] = SysWrite
	cpu.Regs[i386.EBX] = uint32(fd)
	cpu.Regs[i386.ECX] = 200
	cpu.Regs[i386.EDX] = 6
	if err := k.Handle(cpu); err != nil || cpu.Regs[i386.EAX] != 6 {
		t.Fatalf("write=(%d,%v)", cpu.Regs[i386.EAX], err)
	}
	cpu.Regs[i386.EAX] = SysLseek
	cpu.Regs[i386.EBX] = uint32(fd)
	cpu.Regs[i386.ECX] = 0
	cpu.Regs[i386.EDX] = 0
	if err := k.Handle(cpu); err != nil || cpu.Regs[i386.EAX] != 0 {
		t.Fatalf("lseek=(%d,%v)", cpu.Regs[i386.EAX], err)
	}
	_ = mem.WriteBytes(100, []byte("/dir/a\x00"))
	_ = mem.WriteBytes(120, []byte("/dir/b\x00"))
	cpu.Regs[i386.EAX] = SysRename
	cpu.Regs[i386.EBX] = 100
	cpu.Regs[i386.ECX] = 120
	if err := k.Handle(cpu); err != nil || cpu.Regs[i386.EAX] != 0 {
		t.Fatalf("rename=(%d,%v)", cpu.Regs[i386.EAX], err)
	}
	cpu.Regs[i386.EAX] = SysAccess
	cpu.Regs[i386.EBX] = 120
	if err := k.Handle(cpu); err != nil || cpu.Regs[i386.EAX] != 0 {
		t.Fatalf("access=(%d,%v)", cpu.Regs[i386.EAX], err)
	}
	cpu.Regs[i386.EAX] = SysGetrandom
	cpu.Regs[i386.EBX] = 400
	cpu.Regs[i386.ECX] = 32
	if err := k.Handle(cpu); err != nil || cpu.Regs[i386.EAX] != 32 {
		t.Fatalf("getrandom=(%d,%v)", cpu.Regs[i386.EAX], err)
	}
	cpu.Regs[i386.EAX] = SysUnlink
	cpu.Regs[i386.EBX] = 120
	if err := k.Handle(cpu); err != nil || cpu.Regs[i386.EAX] != 0 {
		t.Fatalf("unlink=(%d,%v)", cpu.Regs[i386.EAX], err)
	}
}
