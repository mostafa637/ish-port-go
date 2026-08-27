package guest

import (
	"debug/elf"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"example.com/ish-go/internal/i386"
	"example.com/ish-go/internal/kernel"
)

func oneSegmentELF(payload []byte) []byte {
	const header = 52
	const phSize = 32
	const fileOffset = 0x100
	const vaddr = 0x1000
	data := make([]byte, fileOffset+len(payload))
	copy(data[:4], []byte{0x7f, 'E', 'L', 'F'})
	data[4] = byte(elf.ELFCLASS32)
	data[5] = byte(elf.ELFDATA2LSB)
	data[6] = 1
	binary.LittleEndian.PutUint16(data[16:], uint16(elf.ET_EXEC))
	binary.LittleEndian.PutUint16(data[18:], uint16(elf.EM_386))
	binary.LittleEndian.PutUint32(data[20:], 1)
	binary.LittleEndian.PutUint32(data[24:], vaddr)
	binary.LittleEndian.PutUint32(data[28:], header)
	binary.LittleEndian.PutUint16(data[40:], header)
	binary.LittleEndian.PutUint16(data[42:], phSize)
	binary.LittleEndian.PutUint16(data[44:], 1)
	ph := data[header : header+phSize]
	binary.LittleEndian.PutUint32(ph[0:], uint32(elf.PT_LOAD))
	binary.LittleEndian.PutUint32(ph[4:], fileOffset)
	binary.LittleEndian.PutUint32(ph[8:], vaddr)
	binary.LittleEndian.PutUint32(ph[12:], vaddr)
	binary.LittleEndian.PutUint32(ph[16:], uint32(len(payload)))
	binary.LittleEndian.PutUint32(ph[20:], uint32(len(payload)))
	binary.LittleEndian.PutUint32(ph[24:], uint32(elf.PF_R|elf.PF_X))
	binary.LittleEndian.PutUint32(ph[28:], 0x1000)
	copy(data[fileOffset:], payload)
	return data
}

func TestProcessExecveReplacesImage(t *testing.T) {
	root := t.TempDir()
	initial := make([]byte, 0x140)
	initial[0] = 0xB8
	binary.LittleEndian.PutUint32(initial[1:], 11)
	initial[5] = 0xBB
	binary.LittleEndian.PutUint32(initial[6:], 0x1100)
	initial[10] = 0xB9
	binary.LittleEndian.PutUint32(initial[11:], 0x1110)
	initial[15] = 0xBA
	binary.LittleEndian.PutUint32(initial[16:], 0x1120)
	initial[20] = 0xCD
	initial[21] = 0x80
	initial[22] = 0xF4
	copy(initial[0x100:], []byte("/next\x00"))
	binary.LittleEndian.PutUint32(initial[0x110:], 0x1100)
	binary.LittleEndian.PutUint32(initial[0x114:], 0)
	binary.LittleEndian.PutUint32(initial[0x120:], 0)
	if err := os.WriteFile(filepath.Join(root, "initial"), oneSegmentELF(initial), 0o700); err != nil {
		t.Fatal(err)
	}
	next := []byte{
		0xBB, 42, 0, 0, 0, // exit status
		0xB8, 1, 0, 0, 0, // SYS_exit
		0xCD, 0x80,
	}
	if err := os.WriteFile(filepath.Join(root, "next"), oneSegmentELF(next), 0o700); err != nil {
		t.Fatal(err)
	}
	p, err := Load(filepath.Join(root, "initial"), []string{"/initial"}, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Run(t.Context(), 100); err != nil {
		t.Fatal(err)
	}
	if p.ExitCode != 42 || p.State != Exited {
		t.Fatalf("replacement image did not exit: state=%v exit=%d", p.State, p.ExitCode)
	}
	if p.Steps < 3 {
		t.Fatalf("unexpected step count: %d", p.Steps)
	}
}

func TestProcessHLTIsFault(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "hlt")
	if err := os.WriteFile(path, oneSegmentELF([]byte{0xF4}), 0o700); err != nil {
		t.Fatal(err)
	}
	p, err := Load(path, []string{"/hlt"}, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Run(t.Context(), 10); err == nil {
		t.Fatal("HLT unexpectedly completed successfully")
	}
	if p.State != Faulted {
		t.Fatalf("state=%v want Faulted", p.State)
	}
}

func TestProcessDefaultSignalTerminatesBeforeInstruction(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "loop")
	// JMP -2: the process would keep running if the signal boundary were skipped.
	if err := os.WriteFile(path, oneSegmentELF([]byte{0xeb, 0xfe}), 0o700); err != nil {
		t.Fatal(err)
	}
	p, err := Load(path, []string{"/loop"}, root)
	if err != nil {
		t.Fatal(err)
	}
	p.Image.CPU.Regs[i386.EAX] = kernel.SysKill
	p.Image.CPU.Regs[i386.EBX] = uint32(p.PID)
	p.Image.CPU.Regs[i386.ECX] = 15 // SIGTERM
	if err := p.Kernel.Handle(p.Image.CPU); err != nil {
		t.Fatal(err)
	}
	if got := int32(p.Image.CPU.Regs[i386.EAX]); got != 0 {
		t.Fatalf("kill returned %d", got)
	}
	if err := p.Step(); err != nil {
		t.Fatal(err)
	}
	if p.State != Exited || p.ExitCode != 143 {
		t.Fatalf("signal termination state=%v exit=%d, want Exited/143", p.State, p.ExitCode)
	}
	if p.Steps != 0 {
		t.Fatalf("signal consumed guest instruction: steps=%d", p.Steps)
	}
}

func TestProcessDeliversHandlerAndReturnsThroughRtSigreturn(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "signal-loop")
	// 0x1000: loop; 0x1002: handler RET; 0x1003: restorer syscall(rt_sigreturn).
	code := []byte{0xeb, 0xfe, 0xc3, 0xb8, 173, 0, 0, 0, 0xcd, 0x80, 0xf4}
	if err := os.WriteFile(path, oneSegmentELF(code), 0o700); err != nil {
		t.Fatal(err)
	}
	p, err := Load(path, []string{"/signal-loop"}, root)
	if err != nil {
		t.Fatal(err)
	}
	const actionAddr = 0x3000
	if _, err := p.Image.Space.MapAnonymous(actionAddr, 0x1000, i386.ProtRead|i386.ProtWrite, "test action"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Image.Space.MapAnonymous(0x4000, 0x1000, i386.ProtRead|i386.ProtWrite, "test mask"); err != nil {
		t.Fatal(err)
	}
	cpu := p.Image.CPU
	if err := cpu.Mem.Write32(actionAddr, 0x1002); err != nil {
		t.Fatal(err)
	}
	if err := cpu.Mem.Write32(actionAddr+4, 0); err != nil {
		t.Fatal(err)
	}
	if err := cpu.Mem.Write32(actionAddr+8, 0x1003); err != nil {
		t.Fatal(err)
	}
	if err := cpu.Mem.Write32(actionAddr+12, 0); err != nil {
		t.Fatal(err)
	}
	if err := cpu.Mem.Write32(actionAddr+16, 0); err != nil {
		t.Fatal(err)
	}
	cpu.Regs[i386.EAX] = kernel.SysRtSigaction
	cpu.Regs[i386.EBX] = 2 // SIGINT
	cpu.Regs[i386.ECX] = actionAddr
	cpu.Regs[i386.EDX] = 0
	cpu.Regs[i386.ESI] = 8
	if err := p.Kernel.Handle(cpu); err != nil || int32(cpu.Regs[i386.EAX]) != 0 {
		t.Fatalf("rt_sigaction=(%d,%v)", int32(cpu.Regs[i386.EAX]), err)
	}
	oldESP := cpu.Regs[i386.ESP]
	cpu.Regs[i386.EAX] = kernel.SysKill
	cpu.Regs[i386.EBX] = uint32(p.PID)
	cpu.Regs[i386.ECX] = 2
	if err := p.Kernel.Handle(cpu); err != nil || int32(cpu.Regs[i386.EAX]) != 0 {
		t.Fatalf("kill=(%d,%v)", int32(cpu.Regs[i386.EAX]), err)
	}
	if err := p.Step(); err != nil {
		t.Fatal(err)
	}
	frameESP := cpu.Regs[i386.ESP]
	if cpu.EIP != 0x1002 || frameESP >= oldESP || frameESP&15 != 0 {
		t.Fatalf("handler entry eip=0x%x esp=0x%x oldesp=0x%x", cpu.EIP, frameESP, oldESP)
	}
	if err := p.Step(); err != nil { // handler RET -> restorer
		t.Fatal(err)
	}
	if cpu.EIP != 0x1003 {
		t.Fatalf("after handler ret eip=0x%x want 0x1003", cpu.EIP)
	}
	if err := p.Step(); err != nil { // mov eax, __NR_rt_sigreturn
		t.Fatal(err)
	}
	if err := p.Step(); err != nil { // int 0x80 -> restore frame
		t.Fatal(err)
	}
	if cpu.EIP != 0x1000 || cpu.Regs[i386.ESP] != oldESP {
		t.Fatalf("after rt_sigreturn eip=0x%x esp=0x%x want eip=0x1000 esp=0x%x", cpu.EIP, cpu.Regs[i386.ESP], oldESP)
	}
	cpu.Regs[i386.EAX] = kernel.SysRtSigprocmask
	cpu.Regs[i386.EBX] = 0 // how is ignored when set is NULL
	cpu.Regs[i386.ECX] = 0
	cpu.Regs[i386.EDX] = 0x4000
	cpu.Regs[i386.ESI] = 8
	if err := p.Kernel.Handle(cpu); err != nil || int32(cpu.Regs[i386.EAX]) != 0 {
		t.Fatalf("read restored signal mask=(%d,%v)", int32(cpu.Regs[i386.EAX]), err)
	}
	maskLow, err := cpu.Mem.Read32(0x4000)
	if err != nil {
		t.Fatal(err)
	}
	maskHigh, err := cpu.Mem.Read32(0x4004)
	if err != nil {
		t.Fatal(err)
	}
	if p.State == Faulted || maskLow != 0 || maskHigh != 0 {
		t.Fatalf("signal return state=%v mask=0x%x:%x fault=%v", p.State, maskHigh, maskLow, p.Fault)
	}
}
