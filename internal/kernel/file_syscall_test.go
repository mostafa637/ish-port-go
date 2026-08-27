package kernel

import (
	"context"
	"testing"
	"time"

	"example.com/ish-go/internal/i386"
	"example.com/ish-go/internal/pty"
	"example.com/ish-go/internal/vfs"
)

func TestOpenReadClose(t *testing.T) {
	root := t.TempDir()
	fsys, err := vfs.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := fsys.WriteFile("/hello", []byte("guest file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mem := i386.NewMemory(8192)
	if err := mem.WriteBytes(100, []byte("/hello\x00")); err != nil {
		t.Fatal(err)
	}
	cpu := i386.NewCPU(mem)
	k := New(fsys, pty.New())
	cpu.Regs[i386.EAX] = SysOpen
	cpu.Regs[i386.EBX] = 100
	cpu.Regs[i386.ECX] = 0
	cpu.Regs[i386.EDX] = 0
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	fd := int(cpu.Regs[i386.EAX])
	if fd < 3 {
		t.Fatalf("open fd=%d", fd)
	}
	cpu.Regs[i386.EAX] = SysRead
	cpu.Regs[i386.EBX] = uint32(fd)
	cpu.Regs[i386.ECX] = 200
	cpu.Regs[i386.EDX] = 32
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	n := int(cpu.Regs[i386.EAX])
	data, err := mem.ReadBytes(200, uint32(n))
	if err != nil || string(data) != "guest file\n" {
		t.Fatalf("read = (%q, %v)", data, err)
	}
	cpu.Regs[i386.EAX] = SysClose
	cpu.Regs[i386.EBX] = uint32(fd)
	if err := k.Handle(cpu); err != nil || cpu.Regs[i386.EAX] != 0 {
		t.Fatalf("close = (%d, %v)", cpu.Regs[i386.EAX], err)
	}
	_ = context.Background()
}

func TestStat64UsesECXOutputPointer(t *testing.T) {
	fsys, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mem := i386.NewMemory(8192)
	if err := mem.WriteBytes(100, []byte("/\x00")); err != nil {
		t.Fatal(err)
	}
	cpu := i386.NewCPU(mem)
	k := New(fsys, pty.New())
	cpu.Regs[i386.EAX] = SysStat64
	cpu.Regs[i386.EBX] = 100
	cpu.Regs[i386.ECX] = 200
	cpu.Regs[i386.EDX] = 0xffffffff // Must not be used as stat buffer.
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != 0 {
		t.Fatalf("stat64 returned %d", got)
	}
	mode, err := mem.Read32(216) // writeStat mode field is at stat+16.
	if err != nil {
		t.Fatal(err)
	}
	if mode&0o170000 != 0o040000 {
		t.Fatalf("stat mode=0%o, want directory", mode)
	}
}

func TestFstatat64AtFDCWD(t *testing.T) {
	fsys, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mem := i386.NewMemory(8192)
	if err := mem.WriteBytes(100, []byte("/\x00")); err != nil {
		t.Fatal(err)
	}
	cpu := i386.NewCPU(mem)
	k := New(fsys, pty.New())
	cpu.Regs[i386.EAX] = SysFstatat64
	cpu.Regs[i386.EBX] = 0xffffff9c // AT_FDCWD (-100)
	cpu.Regs[i386.ECX] = 100
	cpu.Regs[i386.EDX] = 300
	cpu.Regs[i386.ESI] = 0
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != 0 {
		t.Fatalf("fstatat64 returned %d", got)
	}
	mode, err := mem.Read32(316) // stat+16
	if err != nil {
		t.Fatal(err)
	}
	if mode&0o170000 != 0o040000 {
		t.Fatalf("fstatat mode=0%o, want directory", mode)
	}
}

func TestStatxAtFDCWD(t *testing.T) {
	fsys, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mem := i386.NewMemory(8192)
	if err := mem.WriteBytes(100, []byte("/\x00")); err != nil {
		t.Fatal(err)
	}
	cpu := i386.NewCPU(mem)
	k := New(fsys, pty.New())
	cpu.Regs[i386.EAX] = SysStatx
	cpu.Regs[i386.EBX] = 0xffffff9c // AT_FDCWD (-100)
	cpu.Regs[i386.ECX] = 100
	cpu.Regs[i386.EDX] = 0x800 // AT_NO_AUTOMOUNT
	cpu.Regs[i386.ESI] = 0x7ff
	cpu.Regs[i386.EDI] = 400
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != 0 {
		t.Fatalf("statx returned %d", got)
	}
	mask, err := mem.Read32(400)
	if err != nil || mask != 0x7ff {
		t.Fatalf("statx mask=(0x%x,%v)", mask, err)
	}
	mode, err := mem.Read16(428) // statx mode is at statx+28.
	if err != nil {
		t.Fatal(err)
	}
	if mode&0o170000 != 0o040000 {
		t.Fatalf("statx mode=0%o, want directory", mode)
	}
}

func TestReadPTYContextCancellation(t *testing.T) {
	fsys, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mem := i386.NewMemory(8192)
	cpu := i386.NewCPU(mem)
	k := New(fsys, pty.New())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	k.SetContext(ctx)
	cpu.Regs[i386.EAX] = SysRead
	cpu.Regs[i386.EBX] = 0
	cpu.Regs[i386.ECX] = 200
	cpu.Regs[i386.EDX] = 1
	done := make(chan error, 1)
	go func() { done <- k.Handle(cpu) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("PTY read did not unblock after context cancellation")
	}
	if got := int32(cpu.Regs[i386.EAX]); got != -ErrnoInterrupted {
		t.Fatalf("read return=%d want=%d", got, -ErrnoInterrupted)
	}
}

func TestFcntlDupFDForPTY(t *testing.T) {
	fsys, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cpu := i386.NewCPU(i386.NewMemory(8192))
	k := New(fsys, pty.New())
	cpu.Regs[i386.EAX] = SysFcntl
	cpu.Regs[i386.EBX] = 0
	cpu.Regs[i386.ECX] = 0 // F_DUPFD
	cpu.Regs[i386.EDX] = 3
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != 3 {
		t.Fatalf("F_DUPFD returned %d want 3", got)
	}
	if _, ok := k.fds[3].(ttyHandle); !ok {
		t.Fatalf("fd 3 is %T, want ttyHandle", k.fds[3])
	}
}

func TestFstat64StandardInput(t *testing.T) {
	fsys, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mem := i386.NewMemory(8192)
	cpu := i386.NewCPU(mem)
	k := New(fsys, pty.New())
	cpu.Regs[i386.EAX] = SysFstat64
	cpu.Regs[i386.EBX] = 0
	cpu.Regs[i386.ECX] = 200
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != 0 {
		t.Fatalf("fstat64(stdin) returned %d", got)
	}
	mode, err := mem.Read32(216)
	if err != nil {
		t.Fatal(err)
	}
	if mode&0o170000 != 0o020000 {
		t.Fatalf("stdin mode=0%o, want character device", mode)
	}
}

func TestDup2AndDup3(t *testing.T) {
	fsys, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cpu := i386.NewCPU(i386.NewMemory(8192))
	k := New(fsys, pty.New())

	cpu.Regs[i386.EAX] = SysDup2
	cpu.Regs[i386.EBX] = 0
	cpu.Regs[i386.ECX] = 7
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != 7 {
		t.Fatalf("dup2 returned %d want 7", got)
	}
	if _, ok := k.fds[7].(ttyHandle); !ok {
		t.Fatalf("fd 7 is %T, want ttyHandle", k.fds[7])
	}

	cpu.Regs[i386.EAX] = SysDup2
	cpu.Regs[i386.EBX] = 7
	cpu.Regs[i386.ECX] = 7
	if err := k.Handle(cpu); err != nil || int32(cpu.Regs[i386.EAX]) != 7 {
		t.Fatalf("dup2 same = (%d,%v)", int32(cpu.Regs[i386.EAX]), err)
	}

	cpu.Regs[i386.EAX] = SysDup3
	cpu.Regs[i386.EBX] = 7
	cpu.Regs[i386.ECX] = 8
	cpu.Regs[i386.EDX] = 0x80000
	if err := k.Handle(cpu); err != nil || int32(cpu.Regs[i386.EAX]) != 8 {
		t.Fatalf("dup3 returned %d err=%v", int32(cpu.Regs[i386.EAX]), err)
	}
	cpu.Regs[i386.EAX] = SysDup3
	cpu.Regs[i386.EBX] = 7
	cpu.Regs[i386.ECX] = 7
	cpu.Regs[i386.EDX] = 0
	if err := k.Handle(cpu); err != nil || int32(cpu.Regs[i386.EAX]) != -ErrnoInvalid {
		t.Fatalf("dup3 same = (%d,%v)", int32(cpu.Regs[i386.EAX]), err)
	}
}

func TestClosedStandardInputIsReusedByOpenAndRead(t *testing.T) {
	root := t.TempDir()
	fsys, err := vfs.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := fsys.WriteFile("/payload", []byte("file-on-zero\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mem := i386.NewMemory(8192)
	if err := mem.WriteBytes(100, []byte("/payload\x00")); err != nil {
		t.Fatal(err)
	}
	cpu := i386.NewCPU(mem)
	k := New(fsys, pty.New())

	cpu.Regs[i386.EAX] = SysClose
	cpu.Regs[i386.EBX] = 0
	if err := k.Handle(cpu); err != nil || int32(cpu.Regs[i386.EAX]) != 0 {
		t.Fatalf("close stdin=(%d,%v)", int32(cpu.Regs[i386.EAX]), err)
	}
	cpu.Regs[i386.EAX] = SysOpen
	cpu.Regs[i386.EBX] = 100
	cpu.Regs[i386.ECX] = 0
	cpu.Regs[i386.EDX] = 0
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != 0 {
		t.Fatalf("open after close(0)=%d want 0", got)
	}

	cpu.Regs[i386.EAX] = SysRead
	cpu.Regs[i386.EBX] = 0
	cpu.Regs[i386.ECX] = 300
	cpu.Regs[i386.EDX] = 32
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	n := int(cpu.Regs[i386.EAX])
	data, err := mem.ReadBytes(300, uint32(n))
	if err != nil || string(data) != "file-on-zero\n" {
		t.Fatalf("read fd0=(%q,%v), want file data", data, err)
	}
}

func TestDup2ReopensClosedDescriptor(t *testing.T) {
	fsys, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cpu := i386.NewCPU(i386.NewMemory(8192))
	k := New(fsys, pty.New())
	cpu.Regs[i386.EAX] = SysDup2
	cpu.Regs[i386.EBX] = 1
	cpu.Regs[i386.ECX] = 0
	if err := k.Handle(cpu); err != nil || int32(cpu.Regs[i386.EAX]) != 0 {
		t.Fatalf("dup2 to closed fd=(%d,%v)", int32(cpu.Regs[i386.EAX]), err)
	}
	if _, ok := k.fds[0].(ttyHandle); !ok {
		t.Fatalf("fd 0 after dup2 is %T, want ttyHandle", k.fds[0])
	}
}

func TestReadvSplitsFileAcrossIovecs(t *testing.T) {
	root := t.TempDir()
	fsys, err := vfs.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := fsys.WriteFile("/data", []byte("abcdefghi"), 0o600); err != nil {
		t.Fatal(err)
	}
	mem := i386.NewMemory(8192)
	if err := mem.WriteBytes(100, []byte("/data\x00")); err != nil {
		t.Fatal(err)
	}
	if err := mem.Write32(200, 300); err != nil {
		t.Fatal(err)
	}
	if err := mem.Write32(204, 4); err != nil {
		t.Fatal(err)
	}
	if err := mem.Write32(208, 400); err != nil {
		t.Fatal(err)
	}
	if err := mem.Write32(212, 8); err != nil {
		t.Fatal(err)
	}
	cpu := i386.NewCPU(mem)
	k := New(fsys, pty.New())
	cpu.Regs[i386.EAX] = SysOpen
	cpu.Regs[i386.EBX] = 100
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	fd := int32(cpu.Regs[i386.EAX])
	if fd < 0 {
		t.Fatalf("open returned %d", fd)
	}
	cpu.Regs[i386.EAX] = SysReadv
	cpu.Regs[i386.EBX] = uint32(fd)
	cpu.Regs[i386.ECX] = 200
	cpu.Regs[i386.EDX] = 2
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != 9 {
		t.Fatalf("readv returned %d want 9", got)
	}
	first, err := mem.ReadBytes(300, 4)
	if err != nil {
		t.Fatal(err)
	}
	second, err := mem.ReadBytes(400, 5)
	if err != nil {
		t.Fatal(err)
	}
	if string(first)+string(second) != "abcdefghi" {
		t.Fatalf("iovecs=%q+%q", first, second)
	}
}
