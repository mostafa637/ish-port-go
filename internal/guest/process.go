package guest

import (
	"context"
	"fmt"
	"os"

	"example.com/ish-go/internal/elf32"
	"example.com/ish-go/internal/i386"
	"example.com/ish-go/internal/kernel"
	"example.com/ish-go/internal/pty"
	"example.com/ish-go/internal/vfs"
)

type State uint8

const (
	Ready State = iota
	Running
	Exited
	Faulted
	Blocked
)

type Process struct {
	Image     *elf32.Image
	Kernel    *kernel.Kernel
	TTY       *pty.Terminal
	FS        *vfs.FS
	State     State
	Fault     error
	ExitCode  int32
	Steps     uint64
	Env       []string
	PID       int32
	ParentPID int32
}

func Load(path string, argv []string, root string) (*Process, error) {
	fsys, err := vfs.New(root)
	if err != nil {
		return nil, err
	}
	tty := pty.New()
	fsys.MountProc("Linux ish-go", 1)
	fsys.MountDev()
	return LoadWithRuntime(path, argv, fsys, tty)
}

func LoadWithRuntime(path string, argv []string, fsys *vfs.FS, tty *pty.Terminal) (*Process, error) {
	if fsys == nil || tty == nil {
		return nil, fmt.Errorf("guest: nil runtime filesystem or tty")
	}
	image, err := elf32.LoadFileWithEnvResolver(path, argv, nil, func(name string) ([]byte, error) {
		host, err := fsys.Resolve(name)
		if err != nil {
			return nil, err
		}
		return os.ReadFile(host)
	})
	if err != nil {
		return nil, err
	}
	k := kernel.New(fsys, tty)
	k.SetAddressSpace(image.Space)
	k.Brk = image.Brk
	k.HeapStart = image.Brk
	k.HeapEnd = image.Brk
	process := &Process{Image: image, Kernel: k, TTY: tty, FS: fsys, State: Ready, PID: 1}
	k.OnExecve = process.replaceImage
	k.Attach(image.CPU)
	return process, nil
}

func (p *Process) Step() error {
	if p.State == Exited {
		return nil
	}
	if p.State == Faulted {
		return p.Fault
	}
	if p.State == Blocked {
		if !p.Kernel.TryResumeBlockedFutex(p.Image.CPU) {
			return nil
		}
		p.State = Ready
	}
	p.State = Running
	if signal, action, ok := p.Kernel.TakePendingSignal(); ok {
		if action.Handler == 0 {
			if signal == 17 || signal == 23 || signal == 28 {
				return nil // Linux default-ignore signals used by terminal/runtime.
			}
			p.Kernel.Exited = true
			p.Kernel.ExitCode = 128 + int32(signal)
			p.State = Exited
			p.ExitCode = p.Kernel.ExitCode
			return nil
		}
		if err := p.Kernel.DeliverSignal(p.Image.CPU, signal, action); err != nil {
			p.State = Faulted
			p.Fault = fmt.Errorf("pid %d: signal %d delivery failed: %w", p.PID, signal, err)
			return p.Fault
		}
		return nil
	}
	startEIP := p.Image.CPU.EIP
	if err := p.Image.CPU.Step(); err != nil {
		p.State = Faulted
		p.Fault = fmt.Errorf("pid %d at eip=0x%08x last=0x%08x op=0x%02x eax=0x%08x ebx=0x%08x ecx=0x%08x edx=0x%08x ebp=0x%08x esp=0x%08x: %w", p.PID, startEIP, p.Image.CPU.LastEIP, p.Image.CPU.LastOpcode, p.Image.CPU.Regs[i386.EAX], p.Image.CPU.Regs[i386.EBX], p.Image.CPU.Regs[i386.ECX], p.Image.CPU.Regs[i386.EDX], p.Image.CPU.Regs[i386.EBP], p.Image.CPU.Regs[i386.ESP], err)
		return p.Fault
	}
	p.Steps++
	if p.Kernel.HasBlockedFutex() {
		p.State = Blocked
		return nil
	}
	if p.Kernel.Exited {
		p.State = Exited
		p.ExitCode = p.Kernel.ExitCode
		return nil
	}
	if p.Image.CPU.Halted {
		p.State = Faulted
		p.Fault = fmt.Errorf("pid %d at eip=0x%08x: guest executed privileged HLT without exit syscall (eax=0x%08x ebx=0x%08x ecx=0x%08x edx=0x%08x edi=0x%08x eflags=0x%08x)", p.PID, startEIP, p.Image.CPU.Regs[i386.EAX], p.Image.CPU.Regs[i386.EBX], p.Image.CPU.Regs[i386.ECX], p.Image.CPU.Regs[i386.EDX], p.Image.CPU.Regs[i386.EDI], p.Image.CPU.EFLAGS)
		return p.Fault
	}
	return nil
}

func (p *Process) Run(ctx context.Context, maxSteps uint64) error {
	if ctx == nil {
		ctx = context.Background()
	}
	p.Kernel.SetContext(ctx)
	defer p.Kernel.SetContext(context.Background())
	for p.State != Exited {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if maxSteps != 0 && p.Steps >= maxSteps {
			return fmt.Errorf("guest instruction limit exceeded: %d", maxSteps)
		}
		if err := p.Step(); err != nil {
			return err
		}
	}
	return nil
}

func (p *Process) CloneForFork(pid int32) *Process {
	space := p.Image.Space.Clone()
	childCPU := *p.Image.CPU
	childCPU.Mem = space.Mem
	childCPU.Regs[i386.EAX] = 0
	childCPU.Halted = false
	childCPU.OnSyscall = nil
	image := &elf32.Image{
		Memory: p.Image.Memory,
		Space:  space,
		CPU:    &childCPU,
		Entry:  p.Image.Entry,
		Stack:  p.Image.Stack,
		Brk:    p.Image.Brk,
		Env:    append([]string(nil), p.Env...),
	}
	image.Memory = space.Mem
	childKernel := p.Kernel.CloneForChild(pid, space)
	child := &Process{Image: image, Kernel: childKernel, TTY: p.TTY, FS: p.FS, State: Ready, PID: pid, ParentPID: p.PID, Env: append([]string(nil), p.Env...)}
	childKernel.OnExecve = child.replaceImage
	childKernel.Attach(image.CPU)
	return child
}

func (p *Process) replaceImage(oldCPU *i386.CPU, guestPath string, argv, envp []string) error {
	hostPath, err := p.FS.Resolve(guestPath)
	if err != nil {
		return err
	}
	if len(argv) == 0 {
		argv = []string{guestPath}
	}
	image, err := elf32.LoadFileWithEnvResolver(hostPath, argv, envp, func(name string) ([]byte, error) {
		host, err := p.FS.Resolve(name)
		if err != nil {
			return nil, err
		}
		return os.ReadFile(host)
	})
	if err != nil {
		return err
	}
	oldCPU.Halted = true
	p.Image = image
	p.Env = append([]string(nil), envp...)
	p.Kernel.Space = image.Space
	p.Kernel.Brk = image.Brk
	p.Kernel.HeapStart = image.Brk
	p.Kernel.HeapEnd = image.Brk
	p.Kernel.Attach(image.CPU)
	return nil
}
