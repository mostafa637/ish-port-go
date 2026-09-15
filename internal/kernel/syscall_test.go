package kernel

import (
	"context"
	"testing"
	"time"

	"example.com/ish-go/internal/i386"
	"example.com/ish-go/internal/pty"
	"example.com/ish-go/internal/vfs"
)

func TestWriteAndGetPID(t *testing.T) {
	mem := i386.NewMemory(4096)
	if err := mem.WriteBytes(100, []byte("OK")); err != nil {
		t.Fatal(err)
	}
	cpu := i386.NewCPU(mem)
	tty := pty.New()
	fs, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	k := New(fs, tty)
	cpu.Regs[i386.EAX] = SysWrite
	cpu.Regs[i386.EBX] = 1
	cpu.Regs[i386.ECX] = 100
	cpu.Regs[i386.EDX] = 2
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if int32(cpu.Regs[i386.EAX]) != 2 {
		t.Fatalf("write result=%d", int32(cpu.Regs[i386.EAX]))
	}
	buf := make([]byte, 2)
	n, err := tty.ReadOutput(context.Background(), buf)
	if err != nil || string(buf[:n]) != "OK" {
		t.Fatalf("PTY output = (%q, %v)", buf[:n], err)
	}
	cpu.Regs[i386.EAX] = SysGetpid
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if cpu.Regs[i386.EAX] != 1 {
		t.Fatalf("getpid=%d", cpu.Regs[i386.EAX])
	}
}

func TestSetThreadAreaUpdatesGSBase(t *testing.T) {
	mem := i386.NewMemory(4096)
	if err := mem.Write32(0x304, 0x00123000); err != nil {
		t.Fatal(err)
	}
	cpu := i386.NewCPU(mem)
	fs, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	k := New(fs, pty.New())
	cpu.Regs[i386.EAX] = SysSetThreadArea
	cpu.Regs[i386.EBX] = 0x300
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if cpu.Regs[i386.EAX] != 0 {
		t.Fatalf("set_thread_area returned %d", int32(cpu.Regs[i386.EAX]))
	}
	if cpu.GSBase != 0x00123000 {
		t.Fatalf("GSBase=0x%x", cpu.GSBase)
	}
}

func TestSetTidAddress(t *testing.T) {
	mem := i386.NewMemory(4096)
	cpu := i386.NewCPU(mem)
	fs, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	k := New(fs, pty.New())
	cpu.Regs[i386.EAX] = SysSetTidAddress
	cpu.Regs[i386.EBX] = 0x380
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if cpu.Regs[i386.EAX] != uint32(k.PID) || k.TidAddress != 0x380 {
		t.Fatalf("return=%d tid_address=0x%x", cpu.Regs[i386.EAX], k.TidAddress)
	}
}

func TestWritev(t *testing.T) {
	mem := i386.NewMemory(4096)
	if err := mem.WriteBytes(100, []byte("A")); err != nil {
		t.Fatal(err)
	}
	if err := mem.WriteBytes(110, []byte("BC")); err != nil {
		t.Fatal(err)
	}
	if err := mem.Write32(0x200, 100); err != nil {
		t.Fatal(err)
	}
	if err := mem.Write32(0x204, 1); err != nil {
		t.Fatal(err)
	}
	if err := mem.Write32(0x208, 110); err != nil {
		t.Fatal(err)
	}
	if err := mem.Write32(0x20c, 2); err != nil {
		t.Fatal(err)
	}
	cpu := i386.NewCPU(mem)
	tty := pty.New()
	fs, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	k := New(fs, tty)
	cpu.Regs[i386.EAX] = SysWritev
	cpu.Regs[i386.EBX] = 1
	cpu.Regs[i386.ECX] = 0x200
	cpu.Regs[i386.EDX] = 2
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != 3 {
		t.Fatalf("writev result=%d", got)
	}
	buf := make([]byte, 3)
	n, err := tty.ReadOutput(context.Background(), buf)
	if err != nil || string(buf[:n]) != "ABC" {
		t.Fatalf("PTY output = (%q, %v)", buf[:n], err)
	}
}

func TestGetgroups32AndSetgroups32(t *testing.T) {
	mem := i386.NewMemory(4096)
	cpu := i386.NewCPU(mem)
	fs, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	k := New(fs, pty.New())

	cpu.Regs[i386.EAX] = SysGetgroups
	cpu.Regs[i386.EBX] = 0
	cpu.Regs[i386.ECX] = 0
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got, want := int32(cpu.Regs[i386.EAX]), int32(1); got != want {
		t.Fatalf("getgroups count=%d want=%d", got, want)
	}

	cpu.Regs[i386.EAX] = SysGetgroups
	cpu.Regs[i386.EBX] = 1
	cpu.Regs[i386.ECX] = 0x100
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got, want := int32(cpu.Regs[i386.EAX]), int32(1); got != want {
		t.Fatalf("getgroups fill=%d want=%d", got, want)
	}
	if got, err := mem.Read32(0x100); err != nil || got != 0 {
		t.Fatalf("group[0]=%d err=%v want=0", got, err)
	}

	cpu.Regs[i386.EAX] = SysSetgroups
	cpu.Regs[i386.EBX] = 1
	cpu.Regs[i386.ECX] = 0x100
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != 0 {
		t.Fatalf("setgroups=%d want=0", got)
	}
}

func TestPollTTYReadiness(t *testing.T) {
	mem := i386.NewMemory(4096)
	cpu := i386.NewCPU(mem)
	tty := pty.New()
	fs, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	k := New(fs, tty)
	if err := mem.Write32(0x100, 0); err != nil {
		t.Fatal(err)
	}
	if err := mem.Write16(0x104, 0x001); err != nil { // POLLIN
		t.Fatal(err)
	}
	if err := mem.Write16(0x106, 0xffff); err != nil {
		t.Fatal(err)
	}
	cpu.Regs[i386.EAX] = SysPoll
	cpu.Regs[i386.EBX] = 0x100
	cpu.Regs[i386.ECX] = 1
	cpu.Regs[i386.EDX] = 0
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != 0 {
		t.Fatalf("empty tty poll=%d want=0", got)
	}
	if got, err := mem.Read16(0x106); err != nil || got != 0 {
		t.Fatalf("empty tty revents=0x%x err=%v", got, err)
	}

	if _, err := tty.WriteInput([]byte("line\n")); err != nil {
		t.Fatal(err)
	}
	cpu.Regs[i386.EAX] = SysPoll
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != 1 {
		t.Fatalf("ready tty poll=%d want=1", got)
	}
	if got, err := mem.Read16(0x106); err != nil || got&0x001 == 0 {
		t.Fatalf("ready tty revents=0x%x err=%v", got, err)
	}

	if err := mem.Write32(0x100, 99); err != nil {
		t.Fatal(err)
	}
	cpu.Regs[i386.EAX] = SysPoll
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != 1 {
		t.Fatalf("bad fd poll=%d want=1", got)
	}
	if got, err := mem.Read16(0x106); err != nil || got&0x008 == 0 {
		t.Fatalf("bad fd revents=0x%x err=%v", got, err)
	}
}

func TestNanosleepTimespecAndCancellation(t *testing.T) {
	mem := i386.NewMemory(4096)
	cpu := i386.NewCPU(mem)
	fs, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	k := New(fs, pty.New())
	if err := mem.Write32(0x100, 0); err != nil {
		t.Fatal(err)
	}
	if err := mem.Write32(0x104, 0); err != nil {
		t.Fatal(err)
	}
	cpu.Regs[i386.EAX] = SysNanosleep
	cpu.Regs[i386.EBX] = 0x100
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != 0 {
		t.Fatalf("zero nanosleep=%d want=0", got)
	}

	if err := mem.Write32(0x104, 1_000_000_000); err != nil {
		t.Fatal(err)
	}
	cpu.Regs[i386.EAX] = SysNanosleep
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != -ErrnoInvalid {
		t.Fatalf("invalid nanosleep=%d want=%d", got, -ErrnoInvalid)
	}
}

func TestFutexWaitWakeAndTimeout(t *testing.T) {
	mem := i386.NewMemory(4096)
	if err := mem.Write32(0x100, 7); err != nil {
		t.Fatal(err)
	}
	cpuWait := i386.NewCPU(mem)
	cpuWake := i386.NewCPU(mem)
	fs, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	k := New(fs, pty.New())

	cpuWait.Regs[i386.EAX] = SysFutex
	cpuWait.Regs[i386.EBX] = 0x100
	cpuWait.Regs[i386.ECX] = 0
	cpuWait.Regs[i386.EDX] = 7
	waitDone := make(chan error, 1)
	go func() { waitDone <- k.Handle(cpuWait) }()
	registered := false
	for i := 0; i < 100; i++ {
		k.futexMu.Lock()
		registered = len(k.futexWaiters[0x100]) != 0
		k.futexMu.Unlock()
		if registered {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !registered {
		t.Fatal("futex waiter was not registered")
	}
	cpuWake.Regs[i386.EAX] = SysFutex
	cpuWake.Regs[i386.EBX] = 0x100
	cpuWake.Regs[i386.ECX] = 1
	cpuWake.Regs[i386.EDX] = 1
	if err := k.Handle(cpuWake); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpuWake.Regs[i386.EAX]); got != 1 {
		t.Fatalf("futex wake=%d want=1", got)
	}
	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("futex waiter was not woken")
	}
	if got := int32(cpuWait.Regs[i386.EAX]); got != 0 {
		t.Fatalf("futex wait=%d want=0", got)
	}

	cpuWait.Regs[i386.EAX] = SysFutex
	cpuWait.Regs[i386.EBX] = 0x100
	cpuWait.Regs[i386.ECX] = 0
	cpuWait.Regs[i386.EDX] = 8
	if err := k.Handle(cpuWait); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpuWait.Regs[i386.EAX]); got != -ErrnoAgain {
		t.Fatalf("futex mismatch=%d want=%d", got, -ErrnoAgain)
	}

	if err := mem.Write32(0x108, 0); err != nil {
		t.Fatal(err)
	}
	if err := mem.Write32(0x10c, 1_000_000); err != nil {
		t.Fatal(err)
	}
	cpuWait.Regs[i386.EAX] = SysFutex
	cpuWait.Regs[i386.EBX] = 0x100
	cpuWait.Regs[i386.ECX] = 0
	cpuWait.Regs[i386.EDX] = 7
	cpuWait.Regs[i386.ESI] = 0x108
	if err := k.Handle(cpuWait); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpuWait.Regs[i386.EAX]); got != -ErrnoTimedOut {
		t.Fatalf("futex timeout=%d want=%d", got, -ErrnoTimedOut)
	}
}

func TestCloneForkStyleAndSharedThreadRejection(t *testing.T) {
	mem := i386.NewMemory(4096)
	cpu := i386.NewCPU(mem)
	fs, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	k := New(fs, pty.New())
	k.OnFork = func(*i386.CPU) (int32, error) { return 42, nil }

	cpu.Regs[i386.EAX] = SysClone
	cpu.Regs[i386.EBX] = 17 // SIGCHLD only: fork-style clone
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != 42 {
		t.Fatalf("fork-style clone=%d want=42", got)
	}

	cpu.Regs[i386.EAX] = SysClone
	cpu.Regs[i386.EBX] = 0x100 | 17 // CLONE_VM|SIGCHLD
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != -ErrnoNoSys {
		t.Fatalf("shared clone=%d want=%d", got, -ErrnoNoSys)
	}
}

func TestCloneSharedForwardsI386Arguments(t *testing.T) {
	mem := i386.NewMemory(4096)
	cpu := i386.NewCPU(mem)
	fs, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	k := New(fs, pty.New())
	var got [5]uint32
	k.OnClone = func(_ *i386.CPU, flags, stack, parentTID, childTID, tls uint32) (int32, error) {
		got = [5]uint32{flags, stack, parentTID, childTID, tls}
		return 77, nil
	}
	cpu.Regs[i386.EAX] = SysClone
	cpu.Regs[i386.EBX] = CloneVM | CloneThread | 17
	cpu.Regs[i386.ECX] = 0x2000
	cpu.Regs[i386.EDX] = 0x300
	cpu.Regs[i386.ESI] = 0x400
	cpu.Regs[i386.EDI] = 0x500
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if int32(cpu.Regs[i386.EAX]) != 77 {
		t.Fatalf("clone result=%d", int32(cpu.Regs[i386.EAX]))
	}
	want := [5]uint32{CloneVM | CloneThread | 17, 0x2000, 0x300, 0x500, 0x400}
	if got != want {
		t.Fatalf("clone args=%#v want %#v", got, want)
	}
}

func TestSignalActionsMasksAndKillValidation(t *testing.T) {
	mem := i386.NewMemory(4096)
	cpu := i386.NewCPU(mem)
	fs, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	k := New(fs, pty.New())
	if err := mem.Write32(0x100, 0x12345678); err != nil {
		t.Fatal(err)
	}
	if err := mem.Write32(0x104, 0x10); err != nil {
		t.Fatal(err)
	}
	if err := mem.Write32(0x108, 0x87654321); err != nil {
		t.Fatal(err)
	}
	if err := mem.Write32(0x10c, 0x2); err != nil {
		t.Fatal(err)
	}
	if err := mem.Write32(0x110, 0); err != nil {
		t.Fatal(err)
	}
	cpu.Regs[i386.EAX] = SysRtSigaction
	cpu.Regs[i386.EBX] = 2
	cpu.Regs[i386.ECX] = 0x100
	cpu.Regs[i386.EDX] = 0
	cpu.Regs[i386.ESI] = 8
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != 0 {
		t.Fatalf("rt_sigaction install=%d", got)
	}
	cpu.Regs[i386.EAX] = SysRtSigaction
	cpu.Regs[i386.EBX] = 2
	cpu.Regs[i386.ECX] = 0
	cpu.Regs[i386.EDX] = 0x140
	cpu.Regs[i386.ESI] = 8
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got, _ := mem.Read32(0x140); got != 0x12345678 {
		t.Fatalf("saved handler=0x%x", got)
	}
	if got, _ := mem.Read32(0x14c); got != 0x2 {
		t.Fatalf("saved mask low=0x%x", got)
	}

	if err := mem.Write32(0x180, 1<<4); err != nil {
		t.Fatal(err)
	}
	if err := mem.Write32(0x184, 0); err != nil {
		t.Fatal(err)
	}
	cpu.Regs[i386.EAX] = SysRtSigprocmask
	cpu.Regs[i386.EBX] = 2 // SIG_SETMASK
	cpu.Regs[i386.ECX] = 0x180
	cpu.Regs[i386.EDX] = 0x190
	cpu.Regs[i386.ESI] = 8
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != 0 {
		t.Fatalf("rt_sigprocmask=%d", got)
	}
	if k.signalMask != 1<<4 {
		t.Fatalf("signal mask=0x%x", k.signalMask)
	}

	cpu.Regs[i386.EAX] = SysKill
	cpu.Regs[i386.EBX] = uint32(k.PID)
	cpu.Regs[i386.ECX] = 2
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != 0 || k.pendingSignals&(1<<1) == 0 {
		t.Fatalf("kill self result=%d pending=0x%x", got, k.pendingSignals)
	}
	cpu.Regs[i386.EAX] = SysKill
	cpu.Regs[i386.EBX] = 99
	cpu.Regs[i386.ECX] = 0
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != -ErrnoNoProcess {
		t.Fatalf("kill missing=%d want=%d", got, -ErrnoNoProcess)
	}
}

func TestPendingSignalSelectionHonorsMask(t *testing.T) {
	k := New(nil, nil)
	k.pendingSignals = (uint64(1) << (15 - 1)) | (uint64(1) << (2 - 1))
	k.signalMask = uint64(1) << (2 - 1)
	k.signalActions[15] = SignalAction{}
	signal, _, ok := k.TakePendingSignal()
	if !ok || signal != 15 {
		t.Fatalf("TakePendingSignal=(%d,%t), want signal 15", signal, ok)
	}
	if _, _, ok := k.TakePendingSignal(); ok {
		t.Fatal("masked signal was delivered")
	}
	k.signalMask = 0
	signal, _, ok = k.TakePendingSignal()
	if !ok || signal != 2 {
		t.Fatalf("unmasked pending signal=(%d,%t), want signal 2", signal, ok)
	}
}

func TestPendingSignalPreservesCustomAction(t *testing.T) {
	k := New(nil, nil)
	k.pendingSignals = uint64(1) << (10 - 1)
	want := SignalAction{Handler: 0x12345678, Flags: 0x4, Restorer: 0x87654321, Mask: 0x20}
	k.signalActions[10] = want
	signal, got, ok := k.TakePendingSignal()
	if !ok || signal != 10 || got != want {
		t.Fatalf("TakePendingSignal=(%d,%+v,%t), want (%d,%+v,true)", signal, got, ok, 10, want)
	}
}

func TestABIArgumentValidationForInfoAndLinks(t *testing.T) {
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

	cpu.Regs[i386.EAX] = SysSysinfo
	cpu.Regs[i386.EBX] = 0xfffffffc
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != -ErrnoFault {
		t.Fatalf("sysinfo bad pointer=%d want=%d", got, -ErrnoFault)
	}

	cpu.Regs[i386.EAX] = SysStatfs64
	cpu.Regs[i386.EBX] = 100
	cpu.Regs[i386.ECX] = 83
	cpu.Regs[i386.EDX] = 200
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != -ErrnoInvalid {
		t.Fatalf("statfs64 short size=%d want=%d", got, -ErrnoInvalid)
	}

	cpu.Regs[i386.EAX] = SysReadlink
	cpu.Regs[i386.EBX] = 100
	cpu.Regs[i386.ECX] = 300
	cpu.Regs[i386.EDX] = 32
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != -ErrnoInvalid {
		t.Fatalf("readlink directory=%d want=%d", got, -ErrnoInvalid)
	}
}

func TestSchedulerAwareFutexDoesNotBlockHost(t *testing.T) {
	mem := i386.NewMemory(4096)
	if err := mem.Write32(0x100, 7); err != nil {
		t.Fatal(err)
	}
	cpuWait := i386.NewCPU(mem)
	cpuWake := i386.NewCPU(mem)
	k := New(nil, pty.New())
	k.SetSchedulerAware(true)
	cpuWait.Regs[i386.EAX] = SysFutex
	cpuWait.Regs[i386.EBX] = 0x100
	cpuWait.Regs[i386.ECX] = 0
	cpuWait.Regs[i386.EDX] = 7
	if err := k.Handle(cpuWait); err != nil {
		t.Fatal(err)
	}
	if !k.HasBlockedFutex() {
		t.Fatal("scheduler-aware FUTEX_WAIT did not register blocked state")
	}
	cpuWake.Regs[i386.EAX] = SysFutex
	cpuWake.Regs[i386.EBX] = 0x100
	cpuWake.Regs[i386.ECX] = 1
	cpuWake.Regs[i386.EDX] = 1
	if err := k.Handle(cpuWake); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpuWake.Regs[i386.EAX]); got != 1 {
		t.Fatalf("futex wake=%d want=1", got)
	}
	if !k.TryResumeBlockedFutex(cpuWait) {
		t.Fatal("woken futex did not resume")
	}
	if k.HasBlockedFutex() || int32(cpuWait.Regs[i386.EAX]) != 0 {
		t.Fatalf("futex resume blocked=%t eax=%d", k.HasBlockedFutex(), int32(cpuWait.Regs[i386.EAX]))
	}
}
