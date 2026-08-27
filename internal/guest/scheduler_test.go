package guest

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"example.com/ish-go/internal/i386"
)

func TestSchedulerForkAndChildExit(t *testing.T) {
	root := t.TempDir()
	code := []byte{
		0xB8, 2, 0, 0, 0, // mov eax, SYS_fork
		0xCD, 0x80,
		0x3D, 0, 0, 0, 0, // cmp eax, 0
		0x74, 0x0D, // child branch
		0xB8, 1, 0, 0, 0, // parent: exit
		0xBB, 7, 0, 0, 0,
		0xCD, 0x80,
		0xF4,
		0xB8, 1, 0, 0, 0, // child: exit
		0xBB, 3, 0, 0, 0,
		0xCD, 0x80,
		0xF4,
	}
	path := filepath.Join(root, "forker")
	if err := os.WriteFile(path, oneSegmentELF(code), 0o700); err != nil {
		t.Fatal(err)
	}
	parent, err := Load(path, []string{"/forker"}, root)
	if err != nil {
		t.Fatal(err)
	}
	s := NewScheduler(parent)
	if err := s.Run(t.Context(), 100); err != nil {
		t.Fatal(err)
	}
	if parent.State != Exited || parent.ExitCode != 7 {
		t.Fatalf("parent state=%v exit=%d", parent.State, parent.ExitCode)
	}
	child, ok := s.Task(2)
	if !ok || child.State != Exited || child.ExitCode != 3 {
		if ok {
			t.Fatalf("child state=%v exit=%d", child.State, child.ExitCode)
		}
		t.Fatal("child task missing")
	}
	if parent.Image.CPU.Regs[i386.EAX] != 0 {
		// Parent's exit syscall leaves EAX at zero; this also catches accidental
		// sharing of the child CPU register file.
		t.Fatalf("parent eax=%d", parent.Image.CPU.Regs[i386.EAX])
	}
	if _, err := parent.Image.Space.MapAnonymous(0x3000, 0x1000, i386.ProtRead|i386.ProtWrite, "test wait status"); err != nil {
		t.Fatal(err)
	}
	if err := parent.Image.Memory.Write32(0x3000, 0); err != nil {
		t.Fatal(err)
	}

	if got := s.wait4(parent, parent.Image.CPU, -1, 0x3000, 0); got != 2 {
		t.Fatalf("wait4 returned %d", got)
	}
	status, err := parent.Image.Memory.Read32(0x3000)
	if err != nil || status != 3<<8 {
		t.Fatalf("wait status=(0x%x,%v)", status, err)
	}
	if _, ok := s.Task(2); ok {
		t.Fatal("reaped child is still visible")
	}
	_ = binary.LittleEndian
}

func TestSchedulerRunHonorsContextForBlockedFutex(t *testing.T) {
	root := t.TempDir()
	code := []byte{
		0xb8, 240, 0, 0, 0, // SYS_futex
		0xbb, 0, 0x20, 0, 0, // uaddr 0x2000
		0xb9, 0, 0, 0, 0, // FUTEX_WAIT
		0xba, 7, 0, 0, 0, // expected value
		0xbe, 0, 0, 0, 0, // no timeout
		0xcd, 0x80,
		0xeb, 0xfe,
	}
	path := filepath.Join(root, "futex-waiter")
	if err := os.WriteFile(path, oneSegmentELF(code), 0o700); err != nil {
		t.Fatal(err)
	}
	process, err := Load(path, []string{"/futex-waiter"}, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := process.Image.Space.MapAnonymous(0x2000, 0x1000, i386.ProtRead|i386.ProtWrite, "test futex"); err != nil {
		t.Fatal(err)
	}
	if err := process.Image.Memory.Write32(0x2000, 7); err != nil {
		t.Fatal(err)
	}
	s := NewScheduler(process)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := s.Run(ctx, 0); err != context.DeadlineExceeded {
		t.Fatalf("blocked scheduler run error=%v want deadline", err)
	}
	if process.State != Blocked || !process.Kernel.HasBlockedFutex() {
		t.Fatalf("blocked process state=%v has_futex=%t", process.State, process.Kernel.HasBlockedFutex())
	}
}
