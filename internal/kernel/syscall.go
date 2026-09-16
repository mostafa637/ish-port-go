package kernel

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"

	"example.com/ish-go/internal/i386"
	identitysys "example.com/ish-go/internal/kernel/syscalls/identity"
	processsys "example.com/ish-go/internal/kernel/syscalls/process"
	randomsys "example.com/ish-go/internal/kernel/syscalls/random"
	systemsys "example.com/ish-go/internal/kernel/syscalls/system"
	timesys "example.com/ish-go/internal/kernel/syscalls/time"
	"example.com/ish-go/internal/pty"
	"example.com/ish-go/internal/vfs"
)

var processStart = time.Now()

const (
	SysExit          = 1
	SysFork          = 2
	SysClone         = 120
	SysKill          = 37
	SysSysinfo       = 116
	SysRtSigreturn   = 173
	SysRead          = 3
	SysPipe          = 42
	SysReadv         = 145
	SysDup2          = 63
	SysWrite         = 4
	SysWritev        = 146
	SysOpen          = 5
	SysClose         = 6
	SysUnlink        = 10
	SysExecve        = 11
	SysChdir         = 12
	SysLseek         = 19
	SysGetpid        = 20
	SysSetpgid       = 57
	SysGetpgrp       = 65
	SysSetsid        = 66
	SysLLseek        = 140
	SysGetsid        = 147
	SysGetpgid       = 132
	SysSigprocmask   = 126
	SysRtSigprocmask = 175
	SysBrk           = 45
	SysAccess        = 33
	SysRename        = 38
	SysMkdir         = 39
	SysIoctl         = 54
	SysFcntl         = 55
	SysGettimeofday  = 78
	SysReadlink      = 85
	SysMunmap        = 91
	SysWait4         = 114
	SysUname         = 122
	SysMprotect      = 125
	SysMremap        = 163
	SysNanosleep     = 162
	SysFutex         = 240
	SysPoll          = 168
	SysGetcwd        = 183
	SysMmap2         = 192
	SysStatfs64      = 268
	SysStat64        = 195
	SysFstat64       = 197
	SysGetuid32      = 199
	SysGetgid32      = 200
	SysGeteuid32     = 201
	SysGetegid32     = 202
	SysGetdents64    = 220
	SysExitGroup     = 252
	SysClockGettime  = 265
	SysOpenat        = 295
	SysFstatat64     = 300
	SysFcntl64       = 221
	SysGettid        = 224
	SysDup3          = 330
	SysPipe2         = 331
	SysStatx         = 383
	SysGetrandom     = 355
	SysGetgroups     = 205
	SysSetgroups     = 206
	SysSetThreadArea = 243
	SysSetTidAddress = 258
	SysRtSigaction   = 174
)

// i386 Linux clone flags used by the thread-aware scheduler.
const (
	CloneVM            = 0x00000100
	CloneFS            = 0x00000200
	CloneFiles         = 0x00000400
	CloneSighand       = 0x00000800
	CloneThread        = 0x00010000
	CloneSettls        = 0x00080000
	CloneParentSettid  = 0x00100000
	CloneChildCleartid = 0x00200000
	CloneChildSettid   = 0x01000000
)

const (
	ErrnoSuccess     = 0
	ErrnoInterrupted = 4
	ErrnoBadFD       = 9
	ErrnoNoEntry     = 2
	ErrnoChild       = 10
	ErrnoAgain       = 11
	ErrnoFault       = 14
	ErrnoInvalid     = 22
	ErrnoNoSys       = 38
	ErrnoNoMem       = 12
	ErrnoTimedOut    = 110
	ErrnoNoProcess   = 3
)

// Kernel implements the stable Linux i386 syscall ABI used by a guest.
// Host files are held behind integer descriptors and are always resolved
// through the VFS root.
type ExecveHandler func(cpu *i386.CPU, path string, argv, envp []string) error
type ForkHandler func(cpu *i386.CPU) (int32, error)
type CloneHandler func(cpu *i386.CPU, flags, childStack, parentTID, childTID, tls uint32) (int32, error)
type Wait4Handler func(cpu *i386.CPU, pid int32, statusAddr, options uint32) int32

type SignalAction struct {
	Handler  uint32
	Flags    uint32
	Restorer uint32
	Mask     uint64
}

type futexBlock struct {
	uaddr    uint32
	waiter   chan struct{}
	deadline time.Time
}

type pipeBlock struct {
	fd    int
	addr  uint32
	count uint32
	write bool
	data  []byte
}

type waitBlock struct {
	pid        int32
	statusAddr uint32
	options    uint32
}

type Kernel struct {
	FS             *vfs.FS
	Space          *i386.AddressSpace
	TTY            *pty.Terminal
	OnExecve       ExecveHandler
	OnFork         ForkHandler
	OnClone        CloneHandler
	OnWait4        Wait4Handler
	OnSyscall      func(number uint32, cpu *i386.CPU)
	OnSyscallDone  func(number uint32, cpu *i386.CPU)
	PID            int32
	Thread         bool
	Brk            uint32
	HeapStart      uint32
	HeapEnd        uint32
	cwd            string
	fds            map[int]fileHandle
	closedFDs      map[int]bool
	fdCloexec      map[int]bool
	nextFD         int
	nextMap        uint32
	Exited         bool
	ExitCode       int32
	TidAddress     uint32
	ctx            context.Context
	futexMu        *sync.Mutex
	futexWaiters   map[uint32][]chan struct{}
	signalMask     uint64
	pendingSignals uint64
	signalActions  map[uint32]SignalAction
	schedulerAware bool
	blockedFutex   *futexBlock
	blockedPipe    *pipeBlock
	blockedWait    *waitBlock
}

func New(fs *vfs.FS, tty *pty.Terminal) *Kernel {
	return &Kernel{FS: fs, TTY: tty, PID: 1, cwd: "/", fds: make(map[int]fileHandle), closedFDs: make(map[int]bool), fdCloexec: make(map[int]bool), nextFD: 3, nextMap: 0x02000000, futexMu: &sync.Mutex{}, futexWaiters: make(map[uint32][]chan struct{}), signalActions: make(map[uint32]SignalAction)}
}

func (k *Kernel) Attach(cpu *i386.CPU) {
	if k.Space == nil {
		k.Space = i386.AddressSpaceFromMemory(cpu.Mem)
	}
	cpu.OnSyscall = k.Handle
}

func (k *Kernel) SetAddressSpace(space *i386.AddressSpace) { k.Space = space }

// SetContext supplies cancellation to blocking guest syscalls such as PTY read.
func (k *Kernel) SetContext(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	k.ctx = ctx
}

func (k *Kernel) SetSchedulerAware(enabled bool) {
	k.schedulerAware = enabled
}

func (k *Kernel) HasBlockedFutex() bool {
	return k.blockedFutex != nil
}

// TryResumeBlockedFutex completes one scheduler-aware FUTEX_WAIT when another
// task wakes it or its relative timeout expires. It never waits on the host.
func (k *Kernel) TryResumeBlockedFutex(cpu *i386.CPU) bool {
	blocked := k.blockedFutex
	if blocked == nil {
		return true
	}
	ready := false
	result := int32(0)
	if !blocked.deadline.IsZero() && !time.Now().Before(blocked.deadline) {
		ready = true
		result = -ErrnoTimedOut
	} else {
		select {
		case <-blocked.waiter:
			ready = true
		default:
		}
	}
	if !ready {
		return false
	}
	k.removeFutexWaiter(blocked.uaddr, blocked.waiter)
	k.blockedFutex = nil
	k.ret(cpu, result)
	return true
}

func (k *Kernel) BlockWait4(pid int32, statusAddr, options uint32) bool {
	if k.blockedWait != nil {
		return false
	}
	k.blockedWait = &waitBlock{pid: pid, statusAddr: statusAddr, options: options}
	return true
}

func (k *Kernel) HasBlocked() bool {
	return k.blockedFutex != nil || k.blockedPipe != nil || k.blockedWait != nil
}

func (k *Kernel) TryResumeBlocked(cpu *i386.CPU) bool {
	if k.blockedWait != nil {
		blocked := k.blockedWait
		k.blockedWait = nil
		if k.OnWait4 == nil {
			k.ret(cpu, -ErrnoNoSys)
			return true
		}
		result := k.OnWait4(cpu, blocked.pid, blocked.statusAddr, blocked.options)
		if result == -ErrnoAgain && k.blockedWait != nil {
			return false
		}
		k.blockedWait = nil
		k.ret(cpu, result)
		return true
	}
	if k.blockedFutex != nil {
		return k.TryResumeBlockedFutex(cpu)
	}
	if k.blockedPipe == nil {
		return true
	}
	blocked := k.blockedPipe
	handle, ok := k.fds[blocked.fd].(*pipeHandle)
	if !ok {
		k.blockedPipe = nil
		k.ret(cpu, -ErrnoBadFD)
		return true
	}
	if blocked.write {
		n, err := handle.WriteNonblocking(blocked.data)
		if errors.Is(err, errPipeWouldBlock) {
			return false
		}
		k.blockedPipe = nil
		if errors.Is(err, syscall.EPIPE) {
			k.queueSignal(13) // SIGPIPE
			k.ret(cpu, -32)
		} else if err != nil {
			k.ret(cpu, -ErrnoFault)
		} else {
			k.ret(cpu, int32(n))
		}
		return true
	}
	buf := make([]byte, capCount(blocked.count))
	n, err := handle.ReadNonblocking(buf)
	if errors.Is(err, errPipeWouldBlock) {
		return false
	}
	k.blockedPipe = nil
	if err != nil && err != io.EOF {
		k.ret(cpu, -ErrnoFault)
		return true
	}
	if err := cpu.Mem.WriteBytes(blocked.addr, buf[:n]); err != nil {
		k.ret(cpu, -ErrnoFault)
	} else {
		k.ret(cpu, int32(n))
	}
	return true
}

func (k *Kernel) context() context.Context {
	if k.ctx == nil {
		return context.Background()
	}
	return k.ctx
}

func (k *Kernel) CloneForChild(pid int32, space *i386.AddressSpace) *Kernel {
	child := New(k.FS, k.TTY)
	child.Space = space
	child.PID = pid
	child.Brk = k.Brk
	child.HeapStart = k.HeapStart
	child.HeapEnd = k.HeapEnd
	child.TidAddress = k.TidAddress
	child.cwd = k.cwd
	child.nextFD = k.nextFD
	for fd, handle := range k.fds {
		child.fds[fd] = cloneHandle(handle)
	}

	for fd, closed := range k.closedFDs {
		child.closedFDs[fd] = closed
	}
	for fd, cloexec := range k.fdCloexec {
		child.fdCloexec[fd] = cloexec
	}

	child.OnExecve = k.OnExecve
	child.OnSyscall = k.OnSyscall
	child.OnSyscallDone = k.OnSyscallDone
	child.ctx = k.ctx
	child.futexMu = k.futexMu
	child.futexWaiters = k.futexWaiters
	child.signalMask = k.signalMask
	child.schedulerAware = k.schedulerAware

	child.pendingSignals = 0
	child.signalActions = make(map[uint32]SignalAction, len(k.signalActions))
	for sig, action := range k.signalActions {
		child.signalActions[sig] = action
	}
	return child
}

// CloneForThread creates the per-thread kernel view required by CLONE_VM.
// Address space, descriptor table, cwd, signal actions, and futex queues are
// deliberately shared; CPU state and the thread id remain private.
func (k *Kernel) CloneForThread(pid int32) *Kernel {
	child := k.CloneForChild(pid, k.Space)
	child.Thread = true
	child.fds = k.fds
	child.closedFDs = k.closedFDs
	child.fdCloexec = k.fdCloexec
	child.signalActions = k.signalActions
	child.signalMask = k.signalMask
	child.pendingSignals = 0
	return child
}

func (k *Kernel) ret(cpu *i386.CPU, value int32) { cpu.Regs[i386.EAX] = uint32(value) }

func capCount(count uint32) int {
	if count > 1<<20 {
		return 1 << 20
	}
	return int(count)
}

func (k *Kernel) Handle(cpu *i386.CPU) error {
	n := cpu.Regs[i386.EAX]
	if k.OnSyscall != nil {
		k.OnSyscall(n, cpu)
	}
	arg := func(i uint8) uint32 { return cpu.Regs[i] }
	switch n {
	case SysExit, SysExitGroup:
		k.Exited = true
		k.ExitCode = int32(arg(i386.EBX))
		cpu.Halted = true
		k.ret(cpu, 0)
	case SysRead:
		k.handleRead(cpu, int(arg(i386.EBX)), arg(i386.ECX), arg(i386.EDX))
	case SysPipe:
		k.pipe(cpu, arg(i386.EBX), 0)
	case SysPipe2:
		k.pipe(cpu, arg(i386.EBX), arg(i386.ECX))
	case SysLseek:
		k.lseek(cpu, int(arg(i386.EBX)), int64(int32(arg(i386.ECX))), int(arg(i386.EDX)))
	case SysLLseek:
		k.llseek(cpu, int(arg(i386.EBX)), uint64(arg(i386.ECX))<<32|uint64(arg(i386.EDX)), arg(i386.ESI), int(arg(i386.EDI)))
	case SysWrite:
		k.handleWrite(cpu, int(arg(i386.EBX)), arg(i386.ECX), arg(i386.EDX))
	case SysWritev:
		k.handleWritev(cpu, int(arg(i386.EBX)), arg(i386.ECX), arg(i386.EDX))
	case SysReadv:
		k.handleReadv(cpu, int(arg(i386.EBX)), arg(i386.ECX), arg(i386.EDX))
	case SysOpen:
		k.open(cpu, arg(i386.EBX), arg(i386.ECX), arg(i386.EDX), -1)
	case SysOpenat:
		k.open(cpu, arg(i386.ECX), arg(i386.EDX), arg(i386.ESI), int32(arg(i386.EBX)))
	case SysFstatat64:
		k.fstatat64(cpu, int32(arg(i386.EBX)), arg(i386.ECX), arg(i386.EDX), arg(i386.ESI))
	case SysStatx:
		k.statx(cpu, int32(arg(i386.EBX)), arg(i386.ECX), arg(i386.EDX), arg(i386.ESI), arg(i386.EDI))
	case SysUnlink:
		k.unlink(cpu, arg(i386.EBX))
	case SysAccess:
		k.access(cpu, arg(i386.EBX))
	case SysMkdir:
		k.mkdir(cpu, arg(i386.EBX), arg(i386.ECX))
	case SysRename:
		k.rename(cpu, arg(i386.EBX), arg(i386.ECX))
	case SysDup2:
		k.dup(cpu, int(arg(i386.EBX)), int(arg(i386.ECX)), 0, false)
	case SysDup3:
		k.dup(cpu, int(arg(i386.EBX)), int(arg(i386.ECX)), arg(i386.EDX), true)
	case SysClose:
		fd := int(arg(i386.EBX))
		if f, ok := k.fds[fd]; ok {
			_ = f.Close()
			delete(k.fds, fd)
			delete(k.closedFDs, fd)
			delete(k.fdCloexec, fd)
			k.ret(cpu, 0)
		} else if fd >= 0 && fd <= 2 && k.TTY != nil && !k.closedFDs[fd] {
			k.closedFDs[fd] = true
			k.ret(cpu, 0)
		} else {
			k.ret(cpu, -ErrnoBadFD)
		}
	case SysGetpid, SysGetpgrp, SysGetsid, SysGettid, SysGetpgid:
		processsys.GetPID(k.PID, cpu)
	case SysSysinfo:
		if !systemsys.Sysinfo(arg(i386.EBX), processStart, cpu) {
			k.ret(cpu, -ErrnoFault)
		} else {
			k.ret(cpu, 0)
		}
	case SysKill:
		k.kill(cpu, int32(arg(i386.EBX)), arg(i386.ECX))
	case SysSigprocmask:
		k.sigprocmask(cpu, arg(i386.EBX), arg(i386.ECX), arg(i386.EDX), 4, false)
	case SysRtSigprocmask:
		k.sigprocmask(cpu, arg(i386.EBX), arg(i386.ECX), arg(i386.EDX), arg(i386.ESI), true)

	case SysSetpgid:
		processsys.SetPGID(cpu)
	case SysSetsid:
		processsys.SetSID(k.PID, cpu)
	case SysIoctl:
		k.ioctl(cpu, int(arg(i386.EBX)), arg(i386.ECX), arg(i386.EDX))
	case SysFcntl, SysFcntl64:
		k.fcntl(cpu, int(arg(i386.EBX)), int(arg(i386.ECX)), arg(i386.EDX))
	case SysStat64:
		k.statPath(cpu, arg(i386.EBX), arg(i386.ECX))
	case SysFstat64:
		k.statFD(cpu, int(arg(i386.EBX)), arg(i386.ECX))
	case SysSetThreadArea:
		k.setThreadArea(cpu, arg(i386.EBX))
	case SysSetTidAddress:
		identitysys.SetTIDAddress(&k.TidAddress, arg(i386.EBX), k.PID, cpu)
	case SysRtSigaction:
		k.rtSigaction(cpu, arg(i386.EBX), arg(i386.ECX), arg(i386.EDX), arg(i386.ESI))
	case SysRtSigreturn:
		if err := k.rtSigreturn(cpu); err != nil {
			k.ret(cpu, -ErrnoFault)
		}

	case SysGetuid32, SysGetgid32, SysGeteuid32, SysGetegid32:
		identitysys.GetID(cpu)
	case SysGetgroups:
		k.getgroups(cpu, arg(i386.EBX), arg(i386.ECX))
	case SysSetgroups:
		k.setgroups(cpu, arg(i386.EBX), arg(i386.ECX))
	case SysBrk:
		requested := arg(i386.EBX)
		if requested == 0 {
			k.ret(cpu, int32(k.Brk))
		} else if k.HeapStart != 0 && requested < k.HeapStart {
			k.ret(cpu, int32(k.Brk))
		} else if requested >= k.FSRootLimit(cpu.Mem.Size()) {
			k.ret(cpu, -ErrnoFault)
		} else {
			if k.Space != nil {
				oldPages := pageAlignUp(k.Brk)
				newPages := pageAlignUp(requested)
				if newPages > oldPages {
					if _, err := k.Space.MapAnonymous(oldPages, newPages-oldPages, i386.ProtRead|i386.ProtWrite, "[heap]"); err != nil {
						k.ret(cpu, -ErrnoNoMem)
						break
					}
					if err := cpu.Mem.WriteBytes(oldPages, make([]byte, newPages-oldPages)); err != nil {
						k.ret(cpu, -ErrnoFault)
						break
					}
				} else if newPages < oldPages && k.HeapEnd >= oldPages {
					_ = k.Space.Remove(newPages, oldPages-newPages)
				}
			}
			k.Brk = requested
			if requested > k.HeapEnd {
				k.HeapEnd = pageAlignUp(requested)
			}
			k.ret(cpu, int32(k.Brk))
		}
	case SysChdir:
		k.chdir(cpu, arg(i386.EBX))
	case SysGetcwd:
		k.getcwd(cpu, arg(i386.EBX), arg(i386.ECX))
	case SysUname:
		if !systemsys.Uname(arg(i386.EBX), cpu) {
			k.ret(cpu, -ErrnoFault)
		} else {
			k.ret(cpu, 0)
		}
	case SysGettimeofday:
		if arg(i386.EBX) == 0 || !timesys.Gettimeofday(arg(i386.EBX), cpu) {
			k.ret(cpu, -ErrnoFault)
		} else {
			k.ret(cpu, 0)
		}
	case SysClockGettime:
		if !timesys.ClockGettime(arg(i386.EBX), arg(i386.ECX), processStart, cpu) {
			k.ret(cpu, -ErrnoFault)
		} else {
			k.ret(cpu, 0)
		}
	case SysGetrandom:
		if n, ok := randomsys.Getrandom(arg(i386.EBX), arg(i386.ECX), cpu); ok {
			k.ret(cpu, int32(n))
		} else {
			k.ret(cpu, -ErrnoFault)
		}
	case SysReadlink:
		k.readlink(cpu, arg(i386.EBX), arg(i386.ECX), arg(i386.EDX))
	case SysGetdents64:
		k.getdents64(cpu, int(arg(i386.EBX)), arg(i386.ECX), arg(i386.EDX))
	case SysNanosleep:
		k.nanosleep(cpu, arg(i386.EBX))
	case SysPoll:
		k.poll(cpu, arg(i386.EBX), arg(i386.ECX), int32(arg(i386.EDX)))
	case SysFutex:
		k.futex(cpu, arg(i386.EBX), arg(i386.ECX), arg(i386.EDX), arg(i386.ESI))
	case SysFork, SysClone:
		if n == SysClone && k.OnClone != nil {
			flags, childStack := arg(i386.EBX), arg(i386.ECX)
			parentTID, tls := arg(i386.EDX), arg(i386.ESI)
			childTID := arg(i386.EDI)
			if flags&(CloneVM|CloneThread) == (CloneVM | CloneThread) {
				childPID, err := k.OnClone(cpu, flags, childStack, parentTID, childTID, tls)
				if err != nil {
					k.ret(cpu, -ErrnoFault)
				} else {
					k.ret(cpu, childPID)
				}
				break
			}
		}
		if k.OnFork == nil {
			k.ret(cpu, -ErrnoNoSys)
			break
		}
		if n == SysClone && arg(i386.EBX)&^uint32(0xff) != 0 {
			// CLONE_VM/CLONE_THREAD require a shared address space and
			// thread lifecycle that this cooperative scheduler does not
			// yet provide. Never emulate them as an unrelated fork.
			k.ret(cpu, -ErrnoNoSys)
			break
		}
		childPID, err := k.OnFork(cpu)
		if err != nil {
			k.ret(cpu, -ErrnoFault)
		} else {
			k.ret(cpu, childPID)
		}

	case SysWait4:
		if k.OnWait4 == nil {
			k.ret(cpu, -ErrnoNoSys)
			break
		}
		pid := int32(arg(i386.EBX))
		statusAddr := arg(i386.ECX)
		options := arg(i386.EDI)
		k.ret(cpu, k.OnWait4(cpu, pid, statusAddr, options))
	case SysExecve:
		k.execve(cpu, arg(i386.EBX), arg(i386.ECX), arg(i386.EDX))
	case SysMmap2:
		k.mmap2(cpu, arg(i386.EBX), arg(i386.ECX), arg(i386.EDX), arg(i386.ESI), int32(arg(i386.EDI)), arg(i386.EBP))
	case SysStatfs64:
		k.statfs64(cpu, arg(i386.EBX), arg(i386.ECX), arg(i386.EDX))
	case SysMunmap:
		k.munmap(cpu, arg(i386.EBX), arg(i386.ECX))
	case SysMprotect:
		k.mprotect(cpu, arg(i386.EBX), arg(i386.ECX), arg(i386.EDX))
	case SysMremap:
		k.mremap(cpu, arg(i386.EBX), arg(i386.ECX), arg(i386.EDX), arg(i386.ESI), arg(i386.EDI))
	default:
		k.ret(cpu, -ErrnoNoSys)
	}
	if k.OnSyscallDone != nil {
		k.OnSyscallDone(n, cpu)
	}
	return nil
}

// TakePendingSignal removes one unblocked pending signal for Process.Step.
// Handler delivery still requires a guest-compatible signal frame; callers must
// not treat a non-default action as delivered.
func (k *Kernel) TakePendingSignal() (uint32, SignalAction, bool) {
	available := k.pendingSignals &^ k.signalMask
	for signal := uint32(1); signal <= 64; signal++ {
		bit := uint64(1) << (signal - 1)
		if available&bit == 0 {
			continue
		}
		k.pendingSignals &^= bit
		return signal, k.signalActions[signal], true
	}
	return 0, SignalAction{}, false
}

func (k *Kernel) queueSignal(signal uint32) {
	if signal == 0 || signal > 64 {
		return
	}
	k.pendingSignals |= uint64(1) << (signal - 1)
}

func (k *Kernel) kill(cpu *i386.CPU, pid int32, signal uint32) {
	if pid != k.PID && pid != 0 && pid != -1 {
		k.ret(cpu, -ErrnoNoProcess)
		return
	}
	if signal > 64 {
		k.ret(cpu, -ErrnoInvalid)
		return
	}
	if signal != 0 {
		k.queueSignal(signal)
	}
	k.ret(cpu, 0)
}

func (k *Kernel) readSignalSet(cpu *i386.CPU, addr uint32, size uint32) (uint64, bool) {
	if addr == 0 {
		return 0, true
	}
	if size < 4 || size > 8 {
		return 0, false
	}
	low, err := cpu.Mem.Read32(addr)
	if err != nil {
		return 0, false
	}
	if size == 4 {
		return uint64(low), true
	}
	high, err := cpu.Mem.Read32(addr + 4)
	if err != nil {
		return 0, false
	}
	return uint64(low) | uint64(high)<<32, true
}

func (k *Kernel) writeSignalSet(cpu *i386.CPU, addr uint32, mask uint64, size uint32) bool {
	if addr == 0 {
		return true
	}
	if size < 4 || size > 8 {
		return false
	}
	if err := cpu.Mem.Write32(addr, uint32(mask)); err != nil {
		return false
	}
	if size == 8 {
		if err := cpu.Mem.Write32(addr+4, uint32(mask>>32)); err != nil {
			return false
		}
	}
	return true
}

func (k *Kernel) sigprocmask(cpu *i386.CPU, how, setAddr, oldAddr, size uint32, realtime bool) {
	if realtime && size != 8 {
		k.ret(cpu, -ErrnoInvalid)
		return
	}
	wordSize := uint32(4)
	if realtime {
		wordSize = 8
	}
	old := k.signalMask
	if !k.writeSignalSet(cpu, oldAddr, old, wordSize) {
		k.ret(cpu, -ErrnoFault)
		return
	}
	if setAddr != 0 {
		set, ok := k.readSignalSet(cpu, setAddr, wordSize)
		if !ok {
			k.ret(cpu, -ErrnoFault)
			return
		}
		switch how {
		case 0: // SIG_BLOCK
			k.signalMask |= set
		case 1: // SIG_UNBLOCK
			k.signalMask &^= set
		case 2: // SIG_SETMASK
			k.signalMask = set
		default:
			k.ret(cpu, -ErrnoInvalid)
			return
		}
	}
	k.ret(cpu, 0)
}

func (k *Kernel) rtSigaction(cpu *i386.CPU, signal, actionAddr, oldActionAddr, size uint32) {
	if signal == 0 || signal > 64 || signal == 9 || signal == 19 || size != 8 {
		k.ret(cpu, -ErrnoInvalid)
		return
	}
	if oldActionAddr != 0 {
		action := k.signalActions[signal]
		if err := cpu.Mem.Write32(oldActionAddr, action.Handler); err != nil {
			k.ret(cpu, -ErrnoFault)
			return
		}
		if err := cpu.Mem.Write32(oldActionAddr+4, action.Flags); err != nil {
			k.ret(cpu, -ErrnoFault)
			return
		}
		if err := cpu.Mem.Write32(oldActionAddr+8, action.Restorer); err != nil {
			k.ret(cpu, -ErrnoFault)
			return
		}
		if !k.writeSignalSet(cpu, oldActionAddr+12, action.Mask, 8) {
			k.ret(cpu, -ErrnoFault)
			return
		}
	}
	if actionAddr != 0 {
		handler, err1 := cpu.Mem.Read32(actionAddr)
		flags, err2 := cpu.Mem.Read32(actionAddr + 4)
		restorer, err3 := cpu.Mem.Read32(actionAddr + 8)
		mask, ok := k.readSignalSet(cpu, actionAddr+12, 8)
		if err1 != nil || err2 != nil || err3 != nil || !ok {
			k.ret(cpu, -ErrnoFault)
			return
		}
		k.signalActions[signal] = SignalAction{Handler: handler, Flags: flags, Restorer: restorer, Mask: mask}
	}
	k.ret(cpu, 0)
}

func (k *Kernel) futex(cpu *i386.CPU, uaddr, op, value, timeoutAddr uint32) {
	const (
		futexWait = 0
		futexWake = 1
	)
	if uaddr&3 != 0 {
		k.ret(cpu, -ErrnoInvalid)
		return
	}
	command := op &^ 128 // FUTEX_PRIVATE_FLAG
	switch command {
	case futexWait:
		current, err := cpu.Mem.Read32(uaddr)

		if err != nil {
			k.ret(cpu, -ErrnoFault)
			return
		}
		if current != value {
			k.ret(cpu, -ErrnoAgain)
			return
		}
		waiter := make(chan struct{})
		var deadline time.Time
		if timeoutAddr != 0 {
			sec, secErr := cpu.Mem.Read32(timeoutAddr)
			nsec, nsecErr := cpu.Mem.Read32(timeoutAddr + 4)
			if secErr != nil || nsecErr != nil || nsec >= 1_000_000_000 {
				k.ret(cpu, -ErrnoFault)
				return
			}
			if uint64(sec) > uint64((1<<63-1)/int64(time.Second)) {
				k.ret(cpu, -ErrnoTimedOut)
				return
			}
			deadline = time.Now().Add(time.Duration(sec)*time.Second + time.Duration(nsec))
		}
		k.futexMu.Lock()
		k.futexWaiters[uaddr] = append(k.futexWaiters[uaddr], waiter)
		k.futexMu.Unlock()
		if k.schedulerAware {
			if k.blockedFutex != nil {
				k.removeFutexWaiter(uaddr, waiter)
				k.ret(cpu, -ErrnoAgain)
				return
			}
			k.blockedFutex = &futexBlock{uaddr: uaddr, waiter: waiter, deadline: deadline}
			return
		}
		ctx := k.context()

		var cancel context.CancelFunc
		var timer *time.Timer
		if timeoutAddr != 0 {
			sec, secErr := cpu.Mem.Read32(timeoutAddr)
			nsec, nsecErr := cpu.Mem.Read32(timeoutAddr + 4)
			if secErr != nil || nsecErr != nil || nsec >= 1_000_000_000 {
				k.removeFutexWaiter(uaddr, waiter)
				k.ret(cpu, -ErrnoFault)
				return
			}
			if uint64(sec) > uint64((1<<63-1)/int64(time.Second)) {
				k.removeFutexWaiter(uaddr, waiter)
				k.ret(cpu, -ErrnoTimedOut)
				return
			}
			ctx, cancel = context.WithCancel(ctx)
			timer = time.NewTimer(time.Duration(sec)*time.Second + time.Duration(nsec))
			defer func() {
				cancel()
				timer.Stop()
			}()
		}
		select {
		case <-waiter:
			k.ret(cpu, 0)
		case <-ctx.Done():
			k.removeFutexWaiter(uaddr, waiter)
			if timer != nil && ctx.Err() == context.DeadlineExceeded {
				k.ret(cpu, -ErrnoTimedOut)
			} else {
				k.ret(cpu, -ErrnoInterrupted)
			}
		case <-func() <-chan time.Time {
			if timer == nil {
				return nil
			}
			return timer.C
		}():
			k.removeFutexWaiter(uaddr, waiter)
			k.ret(cpu, -ErrnoTimedOut)
		}
	case futexWake:
		limit := int(value)
		if limit < 0 {
			limit = 0
		}
		k.futexMu.Lock()
		waiters := k.futexWaiters[uaddr]
		if limit > len(waiters) {
			limit = len(waiters)
		}
		for i := 0; i < limit; i++ {
			close(waiters[i])
		}
		if limit == len(waiters) {
			delete(k.futexWaiters, uaddr)
		} else {
			k.futexWaiters[uaddr] = waiters[limit:]
		}
		k.futexMu.Unlock()
		k.ret(cpu, int32(limit))
	default:
		k.ret(cpu, -ErrnoNoSys)
	}
}

func (k *Kernel) removeFutexWaiter(uaddr uint32, waiter chan struct{}) {
	k.futexMu.Lock()
	defer k.futexMu.Unlock()
	waiters := k.futexWaiters[uaddr]
	for i, candidate := range waiters {
		if candidate == waiter {
			waiters = append(waiters[:i], waiters[i+1:]...)
			if len(waiters) == 0 {
				delete(k.futexWaiters, uaddr)
			} else {
				k.futexWaiters[uaddr] = waiters
			}
			return
		}
	}
}

func (k *Kernel) nanosleep(cpu *i386.CPU, reqAddr uint32) {
	sec, err := cpu.Mem.Read32(reqAddr)
	if err != nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	nsec, err := cpu.Mem.Read32(reqAddr + 4)
	if err != nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	if nsec >= 1_000_000_000 || uint64(sec) > uint64((1<<63-1)/int64(time.Second)) {
		k.ret(cpu, -ErrnoInvalid)
		return
	}
	duration := time.Duration(sec)*time.Second + time.Duration(nsec)
	if duration == 0 {
		k.ret(cpu, 0)
		return
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		k.ret(cpu, 0)
	case <-k.context().Done():
		k.ret(cpu, -ErrnoInterrupted)
	}
}

func (k *Kernel) poll(cpu *i386.CPU, fdsAddr, nfds uint32, timeout int32) {
	const (
		pollIn  = 0x001
		pollOut = 0x004
		pollErr = 0x008
		pollHup = 0x010
	)
	if fdsAddr > cpu.Mem.Size() || nfds > 1<<16 || nfds > (cpu.Mem.Size()-fdsAddr)/8 {
		k.ret(cpu, -ErrnoFault)
		return
	}
	check := func() (int32, bool) {
		ready := int32(0)
		for i := uint32(0); i < nfds; i++ {
			addr := fdsAddr + i*8
			fd, err1 := cpu.Mem.Read32(addr)
			events, err2 := cpu.Mem.Read16(addr + 4)
			if err1 != nil || err2 != nil {
				return -ErrnoFault, true
			}
			events &= pollIn | pollOut
			eventsOut := uint16(0)
			handle, ok := k.handleForFD(int(int32(fd)))
			if !ok {
				eventsOut = pollErr
			} else if tty, isTTY := handle.(ttyHandle); isTTY {
				if events&pollIn != 0 && tty.tty.InputReady() {
					eventsOut |= pollIn
				}
				if events&pollOut != 0 {
					eventsOut |= pollOut
				}
			} else if pipe, isPipe := handle.(*pipeHandle); isPipe {
				if events&pollIn != 0 && pipe.ReadReady() {
					eventsOut |= pollIn
				}
				if events&pollOut != 0 && pipe.WriteReady() {
					eventsOut |= pollOut
				}
				if pipe.Hangup() {
					eventsOut |= pollHup
				}
			} else {

				// Regular files are immediately readable and writable.
				eventsOut = events
			}
			if eventsOut != 0 {
				ready++
			}
			if err := cpu.Mem.Write16(addr+6, eventsOut); err != nil {
				return -ErrnoFault, true
			}
		}
		return ready, ready != 0
	}
	if ready, done := check(); done || timeout == 0 {
		k.ret(cpu, ready)
		return
	}
	ctx := k.context()
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
		defer cancel()
	}
	if k.TTY != nil {
		_ = k.TTY.WaitInput(ctx)
	} else if timeout < 0 {
		<-ctx.Done()
	} else {
		timer := time.NewTimer(time.Duration(timeout) * time.Millisecond)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
		}
	}
	ready, _ := check()
	k.ret(cpu, ready)
}

func (k *Kernel) getgroups(cpu *i386.CPU, size, listAddr uint32) {
	// The sandboxed guest runs as uid/gid 0 and has one supplementary group.
	// Match Linux getgroups(0, NULL) by reporting the count without touching
	// the list, and validate the destination for the non-zero form.
	const groupCount = 1
	if size == 0 {
		k.ret(cpu, groupCount)
		return
	}
	if size < groupCount {
		k.ret(cpu, -ErrnoInvalid)
		return
	}
	if cpu.Mem.Write32(listAddr, 0) != nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	k.ret(cpu, groupCount)
}

func (k *Kernel) setgroups(cpu *i386.CPU, size, listAddr uint32) {
	if size > 1<<16 {
		k.ret(cpu, -ErrnoInvalid)
		return
	}
	if size > 0 {
		if _, err := cpu.Mem.ReadBytes(listAddr, size*4); err != nil {
			k.ret(cpu, -ErrnoFault)
			return
		}
	}
	k.ret(cpu, 0)
}

func (k *Kernel) setThreadArea(cpu *i386.CPU, descAddr uint32) {
	base, err := cpu.Mem.Read32(descAddr + 4)
	if err != nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	cpu.GSBase = base
	k.ret(cpu, 0)
}

func (k *Kernel) handleRead(cpu *i386.CPU, fd int, addr, count uint32) {
	buf := make([]byte, capCount(count))
	var n int
	var err error
	if f, ok := k.fds[fd]; ok {
		if _, isTTY := f.(ttyHandle); isTTY && k.TTY != nil {
			n, err = k.TTY.ReadInput(k.context(), buf)
		} else if pipe, isPipe := f.(*pipeHandle); isPipe && k.schedulerAware {
			n, err = pipe.ReadNonblocking(buf)
			if errors.Is(err, errPipeWouldBlock) {
				if k.blockedPipe == nil {
					k.blockedPipe = &pipeBlock{fd: fd, addr: addr, count: count}
					return
				}
				k.ret(cpu, -ErrnoAgain)
				return
			}
		} else {
			n, err = f.Read(buf)
		}
	} else if fd == 0 && k.TTY != nil && !k.closedFDs[fd] {
		n, err = k.TTY.ReadInput(k.context(), buf)
	} else {
		k.ret(cpu, -ErrnoBadFD)
		return
	}
	if err != nil && err != io.EOF {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			k.ret(cpu, -ErrnoInterrupted)
		} else if errors.Is(err, syscall.EAGAIN) {
			k.ret(cpu, -ErrnoAgain)
		} else {
			k.ret(cpu, -ErrnoFault)
		}
		return
	}
	if err := cpu.Mem.WriteBytes(addr, buf[:n]); err != nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	k.ret(cpu, int32(n))
}

func (k *Kernel) lseek(cpu *i386.CPU, fd int, offset int64, whence int) {
	f, ok := k.handleForFD(fd)
	if !ok {
		k.ret(cpu, -ErrnoBadFD)
		return
	}
	pos, err := f.Seek(offset, whence)
	if err != nil {
		k.ret(cpu, -ErrnoInvalid)
		return
	}
	k.ret(cpu, int32(pos))
}

func (k *Kernel) llseek(cpu *i386.CPU, fd int, offset uint64, resultAddr uint32, whence int) {
	f, ok := k.handleForFD(fd)
	if !ok {
		k.ret(cpu, -ErrnoBadFD)
		return
	}
	pos, err := f.Seek(int64(offset), whence)
	if err != nil {
		k.ret(cpu, -ErrnoInvalid)
		return
	}
	if cpu.Mem.Write32(resultAddr, uint32(pos)) != nil || cpu.Mem.Write32(resultAddr+4, uint32(uint64(pos)>>32)) != nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	k.ret(cpu, 0)
}

func (k *Kernel) handleReadv(cpu *i386.CPU, fd int, iovAddr, iovCount uint32) {
	count := capCount(iovCount)
	var total uint32
	for i := 0; i < count; i++ {
		base, err := cpu.Mem.Read32(iovAddr + uint32(i*8))
		if err != nil {
			if total == 0 {
				k.ret(cpu, -ErrnoFault)
			} else {
				k.ret(cpu, int32(total))
			}
			return
		}
		length, err := cpu.Mem.Read32(iovAddr + uint32(i*8+4))
		if err != nil {
			if total == 0 {
				k.ret(cpu, -ErrnoFault)
			} else {
				k.ret(cpu, int32(total))
			}
			return
		}
		buf := make([]byte, capCount(length))
		var n int
		var readErr error
		if f, ok := k.fds[fd]; ok {
			if _, isTTY := f.(ttyHandle); isTTY && k.TTY != nil {
				n, readErr = k.TTY.ReadInput(k.context(), buf)
			} else {
				n, readErr = f.Read(buf)
			}
		} else if fd == 0 && k.TTY != nil && !k.closedFDs[fd] {
			n, readErr = k.TTY.ReadInput(k.context(), buf)
		} else {
			if total == 0 {
				k.ret(cpu, -ErrnoBadFD)
			} else {
				k.ret(cpu, int32(total))
			}
			return
		}
		if readErr != nil && readErr != io.EOF {
			if errors.Is(readErr, context.Canceled) || errors.Is(readErr, context.DeadlineExceeded) {
				if total == 0 {
					k.ret(cpu, -ErrnoInterrupted)
				} else {
					k.ret(cpu, int32(total))
				}
			} else if errors.Is(readErr, syscall.EAGAIN) {
				if total == 0 {
					k.ret(cpu, -ErrnoAgain)
				} else {
					k.ret(cpu, int32(total))
				}
				return
			} else if total == 0 {
				k.ret(cpu, -ErrnoFault)
			} else {
				k.ret(cpu, int32(total))
			}
			return
		}
		if err := cpu.Mem.WriteBytes(base, buf[:n]); err != nil {
			if total == 0 {
				k.ret(cpu, -ErrnoFault)
			} else {
				k.ret(cpu, int32(total))
			}
			return
		}
		total += uint32(n)
		if n != len(buf) || readErr == io.EOF {
			break
		}
	}
	k.ret(cpu, int32(total))
}

func (k *Kernel) handleWritev(cpu *i386.CPU, fd int, iovAddr, iovCount uint32) {
	count := capCount(iovCount)
	var total uint32
	for i := 0; i < count; i++ {
		base, err := cpu.Mem.Read32(iovAddr + uint32(i*8))
		if err != nil {
			if total == 0 {
				k.ret(cpu, -ErrnoFault)
			} else {
				k.ret(cpu, int32(total))
			}
			return
		}
		length, err := cpu.Mem.Read32(iovAddr + uint32(i*8+4))
		if err != nil {
			if total == 0 {
				k.ret(cpu, -ErrnoFault)
			} else {
				k.ret(cpu, int32(total))
			}
			return
		}
		data, err := cpu.Mem.ReadBytes(base, length)
		if err != nil {
			if total == 0 {
				k.ret(cpu, -ErrnoFault)
			} else {
				k.ret(cpu, int32(total))
			}
			return
		}
		written, writeErr := k.writeBytes(fd, data)
		total += uint32(written)
		if writeErr != nil {
			if total == uint32(written) {
				k.ret(cpu, -ErrnoFault)
			} else {
				k.ret(cpu, int32(total))
			}
			return
		}
		if written != len(data) {
			k.ret(cpu, int32(total))
			return
		}
	}
	k.ret(cpu, int32(total))
}

func (k *Kernel) writeBytes(fd int, data []byte) (int, error) {
	if (fd == 1 || fd == 2) && k.TTY != nil {
		return k.TTY.WriteOutput(data)
	}
	if f, ok := k.fds[fd]; ok {
		n, err := f.Write(data)
		if errors.Is(err, syscall.EPIPE) {
			k.queueSignal(13) // SIGPIPE
		}
		return n, err
	}
	return 0, fmt.Errorf("bad file descriptor")
}

func (k *Kernel) handleWrite(cpu *i386.CPU, fd int, addr, count uint32) {
	data, err := cpu.Mem.ReadBytes(addr, uint32(capCount(count)))
	if err != nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	if f, ok := k.fds[fd].(*pipeHandle); ok && k.schedulerAware {
		n, writeErr := f.WriteNonblocking(data)
		if errors.Is(writeErr, errPipeWouldBlock) {
			if k.blockedPipe == nil {
				k.blockedPipe = &pipeBlock{fd: fd, addr: addr, count: count, write: true, data: append([]byte(nil), data...)}
				return
			}
			k.ret(cpu, -ErrnoAgain)
			return
		}
		if writeErr != nil {
			if errors.Is(writeErr, syscall.EPIPE) {
				k.queueSignal(13) // SIGPIPE
				k.ret(cpu, -32)

			} else {
				k.ret(cpu, -ErrnoFault)
			}
			return
		}
		k.ret(cpu, int32(n))
		return
	}
	n, err := k.writeBytes(fd, data)
	if err != nil {
		if errors.Is(err, syscall.EPIPE) {
			k.ret(cpu, -32) // EPIPE; SIGPIPE delivery is a later signal layer.
		} else if errors.Is(err, syscall.EAGAIN) {
			k.ret(cpu, -ErrnoAgain)
		} else {
			k.ret(cpu, -ErrnoFault)
		}
		return
	}
	k.ret(cpu, int32(n))
}

func (k *Kernel) access(cpu *i386.CPU, pathAddr uint32) {
	name, err := cpu.Mem.ReadCString(pathAddr, 4096)
	name = k.guestPath(name)
	if err != nil || k.FS == nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	if _, err := k.FS.Stat(name); err != nil {
		k.ret(cpu, -ErrnoNoEntry)
		return
	}
	k.ret(cpu, 0)
}

func (k *Kernel) mkdir(cpu *i386.CPU, pathAddr, mode uint32) {
	name, err := cpu.Mem.ReadCString(pathAddr, 4096)
	name = k.guestPath(name)
	if err != nil || k.FS == nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	host, err := k.FS.Resolve(name)
	if err != nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	if err := os.Mkdir(host, os.FileMode(mode)&0o777); err != nil {
		if os.IsExist(err) {
			k.ret(cpu, -ErrnoInvalid)
		} else {
			k.ret(cpu, -ErrnoFault)
		}
		return
	}
	k.ret(cpu, 0)
}

func (k *Kernel) unlink(cpu *i386.CPU, pathAddr uint32) {
	name, err := cpu.Mem.ReadCString(pathAddr, 4096)
	name = k.guestPath(name)
	if err != nil || k.FS == nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	host, err := k.FS.Resolve(name)
	if err != nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	if err := os.Remove(host); err != nil {
		if os.IsNotExist(err) {
			k.ret(cpu, -ErrnoNoEntry)
		} else {
			k.ret(cpu, -ErrnoFault)
		}
		return
	}
	k.ret(cpu, 0)
}

func (k *Kernel) rename(cpu *i386.CPU, oldAddr, newAddr uint32) {
	oldName, err := cpu.Mem.ReadCString(oldAddr, 4096)
	if err != nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	newName, err := cpu.Mem.ReadCString(newAddr, 4096)
	oldName = k.guestPath(oldName)
	newName = k.guestPath(newName)
	if err != nil || k.FS == nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	oldHost, err := k.FS.Resolve(oldName)
	if err != nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	newHost, err := k.FS.Resolve(newName)
	if err != nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	if err := os.Rename(oldHost, newHost); err != nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	k.ret(cpu, 0)
}

func (k *Kernel) open(cpu *i386.CPU, pathAddr, flags, mode uint32, _ int32) {
	name, err := cpu.Mem.ReadCString(pathAddr, 4096)
	name = k.guestPath(name)
	if err != nil || k.FS == nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	var f fileHandle
	if data, virtual, virtualErr := k.FS.ReadVirtual(name); virtual || virtualErr != nil {
		if virtualErr != nil {
			k.ret(cpu, -ErrnoFault)
			return
		}
		f = &virtualHandle{name: name, data: data}
	}
	if f == nil {
		host, resolveErr := k.FS.Resolve(name)
		if resolveErr != nil {
			k.ret(cpu, -ErrnoFault)
			return
		}
		switch name {
		case "/dev/null":
			f = nullHandle{}
		case "/dev/zero":
			f = zeroHandle{}
		case "/dev/tty":
			if k.TTY == nil {
				k.ret(cpu, -ErrnoBadFD)
				return
			}
			f = ttyHandle{tty: k.TTY}
		default:
			opened, openErr := os.OpenFile(host, openFlags(flags), os.FileMode(mode)&0o777)
			if openErr != nil {
				if os.IsNotExist(openErr) {
					k.ret(cpu, -ErrnoNoEntry)
				} else {
					k.ret(cpu, -ErrnoFault)
				}
				return
			}
			f = opened
		}
	}
	fd := k.allocateFD(0)
	k.fds[fd] = f
	delete(k.closedFDs, fd)
	if fd >= k.nextFD {
		k.nextFD = fd + 1
	}
	k.ret(cpu, int32(fd))
}

func (k *Kernel) allocateFD(start int) int {
	if start < 0 {
		start = 0
	}
	for fd := start; ; fd++ {
		if _, exists := k.fds[fd]; exists {
			continue
		}
		if fd <= 2 && k.TTY != nil && !k.closedFDs[fd] {
			continue
		}
		return fd
	}
}

func openFlags(flags uint32) int {
	out := os.O_RDONLY
	switch flags & 3 {
	case 1:
		out = os.O_WRONLY
	case 2:
		out = os.O_RDWR
	}
	if flags&64 != 0 {
		out |= os.O_CREATE
	}
	if flags&128 != 0 {
		out |= os.O_EXCL
	}
	if flags&512 != 0 {
		out |= os.O_TRUNC
	}
	if flags&1024 != 0 {
		out |= os.O_APPEND
	}
	return out
}

func (k *Kernel) execve(cpu *i386.CPU, pathAddr, argvAddr, envpAddr uint32) {
	name, err := cpu.Mem.ReadCString(pathAddr, 4096)
	if err != nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	argv, err := readVector(cpu, argvAddr, 4096)
	if err != nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	envp, err := readVector(cpu, envpAddr, 4096)
	if err != nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	if k.OnExecve == nil {
		k.ret(cpu, -ErrnoNoSys)
		return
	}
	if err := k.OnExecve(cpu, k.guestPath(name), argv, envp); err != nil {
		if os.IsNotExist(err) {
			k.ret(cpu, -ErrnoNoEntry)
		} else {
			k.ret(cpu, -ErrnoFault)
		}
	}
}

func readVector(cpu *i386.CPU, addr uint32, max int) ([]string, error) {
	if addr == 0 {
		return nil, nil
	}
	out := make([]string, 0, 8)
	for i := 0; i < max; i++ {
		ptr, err := cpu.Mem.Read32(addr + uint32(i*4))
		if err != nil {
			return nil, err
		}
		if ptr == 0 {
			return out, nil
		}
		value, err := cpu.Mem.ReadCString(ptr, 1<<20)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return nil, fmt.Errorf("guest vector exceeds %d entries", max)
}

func (k *Kernel) guestPath(name string) string {
	if strings.HasPrefix(name, "/") {
		return name
	}
	return path.Join(k.cwd, name)
}

func (k *Kernel) GuestPath(name string) string { return k.guestPath(name) }

func (k *Kernel) chdir(cpu *i386.CPU, addr uint32) {
	name, err := cpu.Mem.ReadCString(addr, 4096)
	name = k.guestPath(name)
	if err != nil || k.FS == nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	if info, err := k.FS.Stat(name); err != nil || !info.IsDir() {
		k.ret(cpu, -ErrnoNoEntry)
		return
	}
	k.cwd = name
	k.ret(cpu, 0)
}

func (k *Kernel) getcwd(cpu *i386.CPU, addr, size uint32) {
	cwd := []byte(k.cwd + "\x00")
	if uint32(len(cwd)) > size || cpu.Mem.WriteBytes(addr, cwd) != nil {
		k.ret(cpu, -ErrnoFault)
	} else {
		k.ret(cpu, int32(addr))
	}
}

func (k *Kernel) getdents64(cpu *i386.CPU, fd int, addr, count uint32) {
	f, ok := k.fds[fd]
	if !ok {
		k.ret(cpu, -ErrnoBadFD)
		return
	}
	entries, err := f.Readdirnames(-1)
	if err != nil && err != io.EOF {
		k.ret(cpu, -ErrnoFault)
		return
	}
	buf := make([]byte, 0, capCount(count))
	for i, name := range entries {
		reclen := 24 + len(name) + 1
		reclen = (reclen + 7) &^ 7
		if len(buf)+reclen > cap(buf) {
			break
		}
		entry := make([]byte, reclen)
		binary.LittleEndian.PutUint64(entry[0:], uint64(i+1))
		binary.LittleEndian.PutUint64(entry[8:], uint64(i+1))
		binary.LittleEndian.PutUint16(entry[16:], uint16(reclen))
		entry[18] = 0
		copy(entry[19:], name)
		buf = append(buf, entry...)
	}
	if err := cpu.Mem.WriteBytes(addr, buf); err != nil {
		k.ret(cpu, -ErrnoFault)
	} else {
		k.ret(cpu, int32(len(buf)))
	}
}

func (k *Kernel) FSRootLimit(memory uint32) uint32 { return memory - 64*1024 }

func (k *Kernel) CloseCloexec() {
	for fd, cloexec := range k.fdCloexec {
		if !cloexec {
			continue
		}
		if handle, ok := k.fds[fd]; ok {
			_ = handle.Close()
			delete(k.fds, fd)
		}
		delete(k.fdCloexec, fd)
		delete(k.closedFDs, fd)
	}
}

func (k *Kernel) CloseOnExit() {
	for fd, handle := range k.fds {
		_ = handle.Close()
		delete(k.fds, fd)
	}
}

func (k *Kernel) String() string {
	return fmt.Sprintf("pid=%d brk=0x%x cwd=%s exited=%t code=%d", k.PID, k.Brk, k.cwd, k.Exited, k.ExitCode)
}

func SyscallName(number uint32) string {
	names := map[uint32]string{
		SysExit: "exit", SysKill: "kill", SysSysinfo: "sysinfo", SysRtSigreturn: "rt_sigreturn", SysRead: "read", SysReadv: "readv", SysDup2: "dup2", SysLseek: "lseek", SysLLseek: "llseek", SysWrite: "write", SysWritev: "writev",
		SysOpen: "open", SysClose: "close", SysAccess: "access", SysMkdir: "mkdir", SysRename: "rename",
		SysUnlink: "unlink", SysIoctl: "ioctl", SysFcntl: "fcntl", SysExecve: "execve", SysChdir: "chdir",
		SysGetpid: "getpid", SysClone: "clone", SysGetpgrp: "getpgrp", SysSetpgid: "setpgid", SysSetsid: "setsid", SysGetsid: "getsid", SysGettid: "gettid", SysGetpgid: "getpgid", SysBrk: "brk", SysMunmap: "munmap", SysMprotect: "mprotect",
		SysUname: "uname", SysGettimeofday: "gettimeofday", SysClockGettime: "clock_gettime",
		SysGetrandom: "getrandom", SysGetgroups: "getgroups", SysSetgroups: "setgroups", SysPoll: "poll", SysFutex: "futex", SysReadlink: "readlink", SysGetdents64: "getdents64", SysOpenat: "openat",
		SysFstatat64: "fstatat64", SysFcntl64: "fcntl64", SysDup3: "dup3", SysPipe: "pipe", SysPipe2: "pipe2", SysStatx: "statx", SysStatfs64: "statfs64", SysNanosleep: "nanosleep", SysGetcwd: "getcwd", SysExitGroup: "exit_group",
	}
	if name, ok := names[number]; ok {
		return name
	}
	return "sys_" + strings.TrimSpace(fmt.Sprint(number))
}

func (k *Kernel) dup(cpu *i386.CPU, oldFD, newFD int, flags uint32, isDup3 bool) {
	if newFD < 0 || oldFD < 0 || (flags != 0 && flags != 0x80000) {
		k.ret(cpu, -ErrnoInvalid)
		return
	}
	handle, ok := k.handleForFD(oldFD)
	if oldFD == newFD {
		if !isDup3 && ok {
			k.ret(cpu, int32(newFD))
		} else {
			k.ret(cpu, -ErrnoInvalid)
		}
		return
	}
	if !ok {
		k.ret(cpu, -ErrnoBadFD)
		return
	}
	if old, exists := k.fds[newFD]; exists && old != handle {
		_ = old.Close()
	}
	k.fds[newFD] = cloneHandle(handle)
	delete(k.closedFDs, newFD)
	delete(k.fdCloexec, newFD)
	if isDup3 && flags&0x80000 != 0 {
		k.fdCloexec[newFD] = true
	}
	if newFD >= k.nextFD {
		k.nextFD = newFD + 1
	}
	k.ret(cpu, int32(newFD))
}

func (k *Kernel) fcntl(cpu *i386.CPU, fd, command int, argument uint32) {
	const (
		fDupFD        = 0
		fGetFD        = 1
		fSetFD        = 2
		fGetFL        = 3
		fSetFL        = 4
		fDupFDCloexec = 1030
	)
	if command == fGetFD || command == fGetFL || command == fSetFD || command == fSetFL {
		if _, ok := k.handleForFD(fd); !ok {
			k.ret(cpu, -ErrnoBadFD)
			return
		}
		switch command {
		case fGetFD:
			if k.fdCloexec[fd] {
				k.ret(cpu, 1) // FD_CLOEXEC
			} else {
				k.ret(cpu, 0)
			}
		case fSetFD:
			if argument&^uint32(1) != 0 {
				k.ret(cpu, -ErrnoInvalid)
				return
			}
			if argument&1 != 0 {
				k.fdCloexec[fd] = true
			} else {
				delete(k.fdCloexec, fd)
			}
			k.ret(cpu, 0)
		default:
			k.ret(cpu, 0)
		}
		return
	}
	if command != fDupFD && command != fDupFDCloexec {

		k.ret(cpu, -ErrnoInvalid)
		return
	}
	if argument > 1<<20 {
		k.ret(cpu, -ErrnoInvalid)
		return
	}
	handle, ok := k.handleForFD(fd)
	if !ok {
		k.ret(cpu, -ErrnoBadFD)
		return
	}
	newFD := k.allocateFD(int(argument))
	k.fds[newFD] = cloneHandle(handle)
	delete(k.closedFDs, newFD)
	delete(k.fdCloexec, newFD)
	if command == fDupFDCloexec {
		k.fdCloexec[newFD] = true
	}
	if newFD >= k.nextFD {
		k.nextFD = newFD + 1
	}
	k.ret(cpu, int32(newFD))
}

func cloneHandle(handle fileHandle) fileHandle {
	if dup, ok := handle.(interface{ CloneHandle() fileHandle }); ok {
		return dup.CloneHandle()
	}
	return handle
}

func (k *Kernel) pipe(cpu *i386.CPU, pipefdAddr, flags uint32) {
	const (
		oNonblock = 0x800
		oCloexec  = 0x80000
	)
	if flags&^(oNonblock|oCloexec) != 0 {
		k.ret(cpu, -ErrnoInvalid)
		return
	}
	readEnd, writeEnd := newPipePair(flags&oNonblock != 0)
	readFD := k.allocateFD(0)
	writeFD := k.allocateFD(readFD + 1)
	k.fds[readFD] = readEnd
	k.fds[writeFD] = writeEnd
	if flags&oCloexec != 0 {
		k.fdCloexec[readFD] = true
		k.fdCloexec[writeFD] = true
	}
	if err := cpu.Mem.Write32(pipefdAddr, uint32(readFD)); err != nil {
		delete(k.fds, readFD)
		delete(k.fds, writeFD)
		delete(k.fdCloexec, readFD)
		delete(k.fdCloexec, writeFD)
		_ = readEnd.Close()
		_ = writeEnd.Close()
		k.ret(cpu, -ErrnoFault)
		return
	}
	if err := cpu.Mem.Write32(pipefdAddr+4, uint32(writeFD)); err != nil {
		delete(k.fds, readFD)
		delete(k.fds, writeFD)
		delete(k.fdCloexec, readFD)
		delete(k.fdCloexec, writeFD)
		_ = readEnd.Close()
		_ = writeEnd.Close()
		k.ret(cpu, -ErrnoFault)
		return
	}
	k.ret(cpu, 0)
}

func (k *Kernel) handleForFD(fd int) (fileHandle, bool) {
	if handle, ok := k.fds[fd]; ok {
		return handle, true
	}
	if k.closedFDs[fd] {
		return nil, false
	}
	if fd >= 0 && fd <= 2 && k.TTY != nil {
		return ttyHandle{tty: k.TTY}, true
	}
	return nil, false
}

func (k *Kernel) ioctl(cpu *i386.CPU, fd int, request, arg uint32) {
	if k.TTY == nil {
		k.ret(cpu, -ErrnoBadFD)
		return
	}
	handle, ok := k.handleForFD(fd)
	if !ok {
		k.ret(cpu, -ErrnoBadFD)
		return
	}
	if _, ok := handle.(ttyHandle); !ok {
		k.ret(cpu, -ErrnoBadFD)
		return
	}
	switch request {
	case 0x5413: // TIOCGWINSZ; rows=24, cols=80, x/y pixels=0.
		winsize := []byte{24, 0, 80, 0, 0, 0, 0, 0}
		if cpu.Mem.WriteBytes(arg, winsize) != nil {
			k.ret(cpu, -ErrnoFault)
			return
		}
	case 0x540f: // TIOCGPGRP
		if cpu.Mem.Write32(arg, uint32(k.PID)) != nil {
			k.ret(cpu, -ErrnoFault)
			return
		}
	case 0x5410: // TIOCSPGRP
		if _, err := cpu.Mem.Read32(arg); err != nil {
			k.ret(cpu, -ErrnoFault)
			return
		}
	case 0x540e: // TIOCSCTTY; one guest session owns its PTY.
	case 0x5401: // TCGETS
		termios := make([]byte, 44)
		// ISIG|ICANON|ECHO in c_lflag, matching the PTY implementation.
		binary.LittleEndian.PutUint32(termios[12:], 0x0000000b)
		if cpu.Mem.WriteBytes(arg, termios) != nil {
			k.ret(cpu, -ErrnoFault)
			return
		}
	case 0x5402, 0x5403, 0x5404: // TCSETS/TCSETSW/TCSETSF
		if _, err := cpu.Mem.ReadBytes(arg, 44); err != nil {
			k.ret(cpu, -ErrnoFault)
			return
		}
	}
	k.ret(cpu, 0)
}

func (k *Kernel) readlink(cpu *i386.CPU, pathAddr, bufAddr, size uint32) {
	name, err := cpu.Mem.ReadCString(pathAddr, 4096)
	name = k.guestPath(name)
	if err != nil || k.FS == nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	target, err := k.FS.Readlink(name)
	if err != nil {
		if os.IsNotExist(err) {
			k.ret(cpu, -ErrnoNoEntry)
		} else if errors.Is(err, syscall.EINVAL) {
			k.ret(cpu, -ErrnoInvalid)
		} else {
			k.ret(cpu, -ErrnoFault)
		}
		return
	}
	data := []byte(target)
	if uint32(len(data)) > size {
		data = data[:size]
	}
	if err := cpu.Mem.WriteBytes(bufAddr, data); err != nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	k.ret(cpu, int32(len(data)))
}

func (k *Kernel) fstatat64(cpu *i386.CPU, dirfd int32, pathAddr, statAddr, flags uint32) {
	if flags != 0 || (dirfd != -100 && dirfd != -1) {
		k.ret(cpu, -ErrnoNoSys)
		return
	}
	k.statPath(cpu, pathAddr, statAddr)
}

func (k *Kernel) statfs64(cpu *i386.CPU, pathAddr, size, statAddr uint32) {
	const statfsSize = 84
	if size < statfsSize {
		k.ret(cpu, -ErrnoInvalid)
		return
	}
	name, err := cpu.Mem.ReadCString(pathAddr, 4096)
	if err != nil || k.FS == nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	if _, err := k.FS.Stat(k.guestPath(name)); err != nil {
		if os.IsNotExist(err) {
			k.ret(cpu, -ErrnoNoEntry)
		} else {
			k.ret(cpu, -ErrnoFault)
		}
		return
	}
	buf := make([]byte, statfsSize)
	binary.LittleEndian.PutUint32(buf[0:], 0xEF53) // Linux-compatible ext family type.
	binary.LittleEndian.PutUint32(buf[4:], 4096)
	binary.LittleEndian.PutUint64(buf[8:], 32768)
	binary.LittleEndian.PutUint64(buf[16:], 16384)
	binary.LittleEndian.PutUint64(buf[24:], 16384)
	binary.LittleEndian.PutUint64(buf[32:], 4096)
	binary.LittleEndian.PutUint64(buf[40:], 2048)
	binary.LittleEndian.PutUint32(buf[56:], 255)
	binary.LittleEndian.PutUint32(buf[60:], 4096)
	if err := cpu.Mem.WriteBytes(statAddr, buf); err != nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	k.ret(cpu, 0)
}

func (k *Kernel) statx(cpu *i386.CPU, dirfd int32, pathAddr, flags, mask, statAddr uint32) {
	if dirfd != -100 && dirfd != -1 {
		k.ret(cpu, -ErrnoNoSys)
		return
	}
	name, err := cpu.Mem.ReadCString(pathAddr, 4096)
	if err != nil || k.FS == nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	info, err := k.FS.Stat(k.guestPath(name))
	if err != nil {
		if os.IsNotExist(err) {
			k.ret(cpu, -ErrnoNoEntry)
		} else {
			k.ret(cpu, -ErrnoFault)
		}
		return
	}
	buf := make([]byte, 256)
	binary.LittleEndian.PutUint32(buf[0:], mask)
	binary.LittleEndian.PutUint32(buf[4:], 4096)
	binary.LittleEndian.PutUint32(buf[16:], 1)
	mode := uint16(linuxMode(info))
	binary.LittleEndian.PutUint16(buf[28:], mode)
	binary.LittleEndian.PutUint64(buf[32:], uint64(info.ModTime().UnixNano()))
	binary.LittleEndian.PutUint64(buf[40:], uint64(info.Size()))
	binary.LittleEndian.PutUint64(buf[48:], uint64((info.Size()+511)/512))
	if cpu.Mem.WriteBytes(statAddr, buf) != nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	k.ret(cpu, 0)
}

func (k *Kernel) statPath(cpu *i386.CPU, pathAddr, statAddr uint32) {
	name, err := cpu.Mem.ReadCString(pathAddr, 4096)
	name = k.guestPath(name)
	if err != nil || k.FS == nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	info, err := k.FS.Stat(name)
	if err != nil {
		if os.IsNotExist(err) {
			k.ret(cpu, -ErrnoNoEntry)
		} else {
			k.ret(cpu, -ErrnoFault)
		}
		return
	}
	k.writeStat(cpu, statAddr, info)
}

func (k *Kernel) statFD(cpu *i386.CPU, fd int, statAddr uint32) {
	f, ok := k.handleForFD(fd)
	if !ok {
		k.ret(cpu, -ErrnoBadFD)
		return
	}
	info, err := f.Stat()
	if err != nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	k.writeStat(cpu, statAddr, info)
}

func linuxMode(info os.FileInfo) uint32 {
	mode := uint32(info.Mode().Perm())
	switch {
	case info.IsDir():
		mode |= 0o040000
	case info.Mode()&os.ModeCharDevice != 0:
		mode |= 0o020000
	case info.Mode()&os.ModeNamedPipe != 0:
		mode |= 0o010000
	case info.Mode()&os.ModeSymlink != 0:
		mode |= 0o120000
	default:
		mode |= 0o100000
	}
	return mode
}

func (k *Kernel) writeStat(cpu *i386.CPU, addr uint32, info os.FileInfo) {
	buf := make([]byte, 104)
	mode := linuxMode(info)
	binary.LittleEndian.PutUint32(buf[16:], mode)
	binary.LittleEndian.PutUint32(buf[20:], 1)
	binary.LittleEndian.PutUint32(buf[24:], 0)
	binary.LittleEndian.PutUint32(buf[28:], 0)
	binary.LittleEndian.PutUint64(buf[44:], uint64(info.Size()))
	binary.LittleEndian.PutUint32(buf[52:], 4096)
	binary.LittleEndian.PutUint64(buf[56:], uint64((info.Size()+511)/512))
	if cpu.Mem.WriteBytes(addr, buf) != nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	k.ret(cpu, 0)
}

func pageAlignUp(v uint32) uint32 {
	const page = uint32(4096)
	if v > ^uint32(0)-(page-1) {
		return ^uint32(0)
	}
	return (v + page - 1) &^ (page - 1)
}

func (k *Kernel) mmap2(cpu *i386.CPU, addr, length, linuxProt, flags uint32, fd int32, pageOffset uint32) {
	if length == 0 || length > 64<<20 || k.Space == nil {
		k.ret(cpu, -ErrnoInvalid)
		return
	}
	length = pageAlignUp(length)
	if length == 0 {
		k.ret(cpu, -ErrnoInvalid)
		return
	}
	mapFixed := flags&0x10 != 0
	mapAnonymous := flags&0x20 != 0
	var fileData []byte
	if !mapAnonymous {
		handle, ok := k.handleForFD(int(fd))
		file, isFile := handle.(*os.File)
		if !ok || !isFile {
			k.ret(cpu, -ErrnoBadFD)
			return
		}
		fileData = make([]byte, length)
		n, readErr := file.ReadAt(fileData, int64(pageOffset)*4096)
		if readErr != nil && readErr != io.EOF {
			k.ret(cpu, -ErrnoFault)
			return
		}
		if n < 0 {
			k.ret(cpu, -ErrnoFault)
			return
		}
	}
	if mapFixed {
		if addr > k.Space.Max || length > k.Space.Max-addr || addr&0xfff != 0 {
			k.ret(cpu, -ErrnoNoMem)
			return
		}
	} else if addr < 0x1000 || addr > k.Space.Max || length > k.Space.Max-addr || addr&0xfff != 0 || !k.Space.IsFree(addr, addr+length) {
		addr = 0
	}

	var oldMappings []i386.Mapping
	var oldData []byte
	if mapFixed {
		oldMappings = append([]i386.Mapping(nil), k.Space.Mappings...)
		if data, err := cpu.Mem.ReadBytes(addr, length); err == nil {
			oldData = data
		} else {
			k.ret(cpu, -ErrnoFault)
			return
		}
	}
	restore := func() {
		if !mapFixed {
			return
		}
		k.Space.Mappings = oldMappings
		_ = cpu.Mem.WriteBytes(addr, oldData)
	}
	if mapFixed {
		if err := k.Space.Remove(addr, length); err != nil {
			restore()
			k.ret(cpu, -ErrnoFault)
			return
		}
	}
	prot := uint8(0)
	if linuxProt&1 != 0 {
		prot |= i386.ProtRead
	}
	if linuxProt&2 != 0 {
		prot |= i386.ProtWrite
	}
	if linuxProt&4 != 0 {
		prot |= i386.ProtExec
	}
	mapped, err := k.Space.MapAnonymous(addr, length, prot, "mmap2")
	if err != nil {
		restore()
		k.ret(cpu, -ErrnoNoMem)
		return
	}
	if mapAnonymous {
		fileData = make([]byte, length)
	}
	if err := cpu.Mem.WriteBytes(mapped, fileData); err != nil {
		_ = k.Space.Remove(mapped, length)
		restore()
		k.ret(cpu, -ErrnoFault)
		return
	}
	k.nextMap = mapped + length
	k.ret(cpu, int32(mapped))
}

func pageAlignLength(length uint32) (uint32, bool) {
	if length == 0 || length > ^uint32(0)-4094 {
		return 0, false
	}
	return (length + 4095) &^ 4095, true
}

func (k *Kernel) munmap(cpu *i386.CPU, addr, length uint32) {
	aligned, ok := pageAlignLength(length)
	if k.Space == nil || !ok || addr&0xfff != 0 {
		k.ret(cpu, -ErrnoInvalid)
		return
	}
	if err := k.Space.Remove(addr, aligned); err != nil {
		k.ret(cpu, -ErrnoInvalid)
		return
	}
	k.ret(cpu, 0)
}

func (k *Kernel) mprotect(cpu *i386.CPU, addr, length, linuxProt uint32) {
	aligned, ok := pageAlignLength(length)
	if k.Space == nil || !ok || addr&0xfff != 0 {
		k.ret(cpu, -ErrnoInvalid)
		return
	}
	prot := uint8(0)
	if linuxProt&1 != 0 {
		prot |= i386.ProtRead
	}
	if linuxProt&2 != 0 {
		prot |= i386.ProtWrite
	}
	if linuxProt&4 != 0 {
		prot |= i386.ProtExec
	}
	length = aligned
	if err := k.Space.Protect(addr, length, prot); err != nil {
		k.ret(cpu, -ErrnoInvalid)
		return
	}
	k.ret(cpu, 0)
}

func (k *Kernel) mremap(cpu *i386.CPU, oldAddr, oldLength, newLength, flags, newAddr uint32) {
	oldSize, oldOK := pageAlignLength(oldLength)
	newSize, newOK := pageAlignLength(newLength)
	if k.Space == nil || !oldOK || !newOK || oldAddr&0xfff != 0 || oldAddr > k.Space.Max || oldSize > k.Space.Max-oldAddr {
		k.ret(cpu, -ErrnoInvalid)
		return
	}
	mapping, ok := k.Space.MappingAt(oldAddr)
	if !ok {
		k.ret(cpu, -ErrnoInvalid)
		return
	}
	oldEnd := oldAddr + oldSize
	coveredEnd := mapping.End
	for coveredEnd < oldEnd {
		next, nextOK := k.Space.MappingAt(coveredEnd)
		if !nextOK || next.Start != coveredEnd || next.Prot != mapping.Prot || next.Name != mapping.Name {
			k.ret(cpu, -ErrnoInvalid)
			return
		}
		coveredEnd = next.End
	}
	if oldSize == newSize {
		k.ret(cpu, int32(oldAddr))
		return
	}
	if newSize < oldSize {
		if err := k.Space.Remove(oldAddr+newSize, oldSize-newSize); err != nil {
			k.ret(cpu, -ErrnoInvalid)
			return
		}
		k.ret(cpu, int32(oldAddr))
		return
	}
	newEnd := oldAddr + newSize
	if newEnd >= oldEnd && newEnd <= k.Space.Max && k.Space.IsFree(oldEnd, newEnd) {
		if err := k.Space.Add(oldEnd, newSize-oldSize, mapping.Prot, mapping.Name); err == nil {
			if err := cpu.Mem.WriteBytes(oldEnd, make([]byte, newSize-oldSize)); err == nil {
				k.ret(cpu, int32(oldAddr))
				return
			}
			_ = k.Space.Remove(oldEnd, newSize-oldSize)
		}
	}
	if flags&1 == 0 { // MREMAP_MAYMOVE
		k.ret(cpu, -ErrnoNoMem)
		return
	}
	oldData, err := cpu.Mem.ReadBytes(oldAddr, oldSize)
	if err != nil {
		k.ret(cpu, -ErrnoFault)
		return
	}
	target := uint32(0)
	fixed := flags&2 != 0 // MREMAP_FIXED
	if fixed {
		target = newAddr
		if target&0xfff != 0 || target > k.Space.Max || newSize > k.Space.Max-target || !k.Space.IsFree(target, target+newSize) {
			k.ret(cpu, -ErrnoInvalid)
			return
		}
	}
	moved, err := k.Space.MapAnonymous(target, newSize, mapping.Prot, mapping.Name)
	if err != nil {
		k.ret(cpu, -ErrnoNoMem)
		return
	}
	if err := cpu.Mem.WriteBytes(moved, make([]byte, newSize)); err != nil {
		_ = k.Space.Remove(moved, newSize)
		k.ret(cpu, -ErrnoFault)
		return
	}
	if err := cpu.Mem.WriteBytes(moved, oldData); err != nil {
		_ = k.Space.Remove(moved, newSize)
		k.ret(cpu, -ErrnoFault)
		return
	}
	if err := k.Space.Remove(oldAddr, oldSize); err != nil {
		_ = k.Space.Remove(moved, newSize)
		k.ret(cpu, -ErrnoFault)
		return
	}
	k.nextMap = moved + newSize
	k.ret(cpu, int32(moved))
}
