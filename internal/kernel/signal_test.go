package kernel

import (
	"testing"

	"example.com/ish-go/internal/i386"
	"example.com/ish-go/internal/pty"
	"example.com/ish-go/internal/vfs"
)

func TestDeliverSignalAndRtSigreturnRestoreCPUState(t *testing.T) {
	mem := i386.NewMemory(0x10000)
	cpu := i386.NewCPU(mem)
	cpu.EIP = 0x12345678
	cpu.EFLAGS = 0x202 | i386.FlagZF
	cpu.Regs[i386.EAX] = 0x11111111
	cpu.Regs[i386.EBX] = 0x22222222
	cpu.Regs[i386.ESP] = 0x8000
	cpu.Regs[i386.ESI] = 0x33333333
	cpu.FSBase = 0x4000
	cpu.GSBase = 0x5000
	cpu.FSSelector = 0x23
	cpu.GSSelector = 0x2b
	cpu.FPU[0] = 3.5
	cpu.FPU[7] = -2.25
	cpu.FPUTop = 3
	cpu.FPUCount = 2
	cpu.FPUStatus = 0x120
	cpu.FPUControl = 0x37f
	fs, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	k := New(fs, pty.New())
	k.signalMask = uint64(1) << (10 - 1)
	action := SignalAction{Handler: 0x2000, Restorer: 0x2100, Mask: uint64(1) << (12 - 1)}
	oldRegs := cpu.Regs
	oldEIP, oldFlags, oldESP := cpu.EIP, cpu.EFLAGS, cpu.Regs[i386.ESP]
	oldFSBase, oldGSBase := cpu.FSBase, cpu.GSBase
	oldMask := k.signalMask
	if err := k.DeliverSignal(cpu, 2, action); err != nil {
		t.Fatal(err)
	}
	frameESP := cpu.Regs[i386.ESP]
	if frameESP&15 != 0 || cpu.EIP != action.Handler || cpu.Regs[i386.ESP] >= oldESP {
		t.Fatalf("delivered frame esp=0x%x eip=0x%x", frameESP, cpu.EIP)
	}
	if got, err := mem.Read32(frameESP); err != nil || got != action.Restorer {
		t.Fatalf("restorer=(0x%x,%v)", got, err)
	}
	if got, err := mem.Read32(frameESP + 4); err != nil || got != 2 {
		t.Fatalf("signal argument=(%d,%v)", got, err)
	}
	if k.signalMask != oldMask|action.Mask|(uint64(1)<<(2-1)) {
		t.Fatalf("handler mask=0x%x", k.signalMask)
	}

	cpu.Regs[i386.EAX] = SysRtSigreturn
	if err := k.Handle(cpu); err != nil {
		t.Fatal(err)
	}
	if got := int32(cpu.Regs[i386.EAX]); got != int32(oldRegs[i386.EAX]) {
		t.Fatalf("rt_sigreturn eax=0x%x want=0x%x", cpu.Regs[i386.EAX], oldRegs[i386.EAX])
	}
	if cpu.EIP != oldEIP || cpu.EFLAGS != oldFlags || cpu.Regs[i386.ESP] != oldESP || cpu.Regs != oldRegs {
		t.Fatalf("CPU state not restored: eip=0x%x flags=0x%x esp=0x%x regs=%v", cpu.EIP, cpu.EFLAGS, cpu.Regs[i386.ESP], cpu.Regs)
	}
	if cpu.FSBase != oldFSBase || cpu.GSBase != oldGSBase || cpu.FPU[0] != 3.5 || cpu.FPU[7] != -2.25 || k.signalMask != oldMask {
		t.Fatalf("extended state not restored: fs=0x%x gs=0x%x fpu=%v mask=0x%x", cpu.FSBase, cpu.GSBase, cpu.FPU, k.signalMask)
	}
	if cpu.Halted {
		t.Fatal("rt_sigreturn left CPU halted")
	}
}

func TestDeliverSignalRejectsUnsupportedForms(t *testing.T) {
	cpu := i386.NewCPU(i386.NewMemory(0x4000))
	cpu.Regs[i386.ESP] = 0x2000
	k := New(nil, nil)
	cases := []SignalAction{
		{Handler: 0x100, Restorer: 0x200, Flags: saSigInfo},
		{Handler: 0x100},
	}
	for _, action := range cases {
		if err := k.DeliverSignal(cpu, 2, action); err == nil {
			t.Fatalf("DeliverSignal(%+v) unexpectedly succeeded", action)
		}
	}
}
