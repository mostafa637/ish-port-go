package kernel

import (
	"fmt"
	"math"

	"example.com/ish-go/internal/i386"
)

const (
	signalFrameMagic   uint32 = 0x49534847 // "ISHG"
	signalFrameVersion uint32 = 1
	signalFrameSize           = 176

	saSigInfo   uint32 = 0x00000004
	saNodefer   uint32 = 0x40000000
	sigRestorer uint32 = 0x04000000
)

// DeliverSignal creates the minimal i386 non-SA_SIGINFO signal frame used by
// this interpreter. The guest handler receives the signal number as its first
// argument and returns through the action's restorer, which must issue
// rt_sigreturn. No host callback or synthetic handler execution is involved.
func (k *Kernel) DeliverSignal(cpu *i386.CPU, signal uint32, action SignalAction) error {
	if cpu == nil || cpu.Mem == nil {
		return fmt.Errorf("signal %d: nil CPU or memory", signal)
	}
	if signal == 0 || signal > 64 || action.Handler == 0 {
		return fmt.Errorf("signal %d: invalid handler", signal)
	}
	if action.Flags&saSigInfo != 0 {
		return fmt.Errorf("signal %d: SA_SIGINFO delivery is not implemented", signal)
	}
	if action.Restorer == 0 {
		return fmt.Errorf("signal %d: handler has no restorer", signal)
	}
	oldESP := cpu.Regs[i386.ESP]
	if oldESP < signalFrameSize+32 {
		return fmt.Errorf("signal %d: guest stack underflow", signal)
	}
	frameBase := (oldESP - signalFrameSize - 16) &^ uint32(15)
	if frameBase+signalFrameSize < frameBase {
		return fmt.Errorf("signal %d: frame address overflow", signal)
	}

	write := func(offset, value uint32) error {
		return cpu.Mem.Write32(frameBase+offset, value)
	}
	if err := write(0, action.Restorer); err != nil {
		return fmt.Errorf("signal %d: write return address: %w", signal, err)
	}
	if err := write(4, signal); err != nil {
		return fmt.Errorf("signal %d: write argument: %w", signal, err)
	}
	if err := write(8, signalFrameMagic); err != nil {
		return err
	}
	if err := write(12, signalFrameVersion); err != nil {
		return err
	}
	if err := write(16, signal); err != nil {
		return err
	}
	if err := write(20, cpu.EIP); err != nil {
		return err
	}
	if err := write(24, cpu.EFLAGS); err != nil {
		return err
	}
	if err := write(28, oldESP); err != nil {
		return err
	}
	if err := write(32, uint32(k.signalMask)); err != nil {
		return err
	}
	if err := write(36, uint32(k.signalMask>>32)); err != nil {
		return err
	}
	if err := write(40, cpu.FSBase); err != nil {
		return err
	}
	if err := write(44, cpu.GSBase); err != nil {
		return err
	}
	if err := write(48, uint32(cpu.FSSelector)); err != nil {
		return err
	}
	if err := write(52, uint32(cpu.GSSelector)); err != nil {
		return err
	}
	if err := write(56, uint32(cpu.FPUTop)); err != nil {
		return err
	}
	if err := write(60, uint32(cpu.FPUCount)); err != nil {
		return err
	}
	if err := write(64, uint32(cpu.FPUStatus)); err != nil {
		return err
	}
	if err := write(68, uint32(cpu.FPUControl)); err != nil {
		return err
	}
	for i, value := range cpu.Regs {
		if err := write(72+uint32(i*4), value); err != nil {
			return err
		}
	}
	for i, value := range cpu.FPU {
		bits := math.Float64bits(value)
		if err := write(104+uint32(i*8), uint32(bits)); err != nil {
			return err
		}
		if err := write(108+uint32(i*8), uint32(bits>>32)); err != nil {
			return err
		}
	}

	newMask := k.signalMask | action.Mask
	if action.Flags&saNodefer == 0 {
		newMask |= uint64(1) << (signal - 1)
	}
	cpu.Regs[i386.ESP] = frameBase
	cpu.EIP = action.Handler
	k.signalMask = newMask
	return nil
}

func (k *Kernel) rtSigreturn(cpu *i386.CPU) error {
	if cpu == nil || cpu.Mem == nil {
		return fmt.Errorf("rt_sigreturn: nil CPU or memory")
	}
	esp := cpu.Regs[i386.ESP]
	candidates := []uint32{esp}
	if esp >= 4 {
		candidates = append(candidates, esp-4)
	}
	var frameBase uint32
	found := false
	for _, candidate := range candidates {
		magic, err := cpu.Mem.Read32(candidate + 8)
		if err == nil && magic == signalFrameMagic {
			version, err := cpu.Mem.Read32(candidate + 12)
			if err == nil && version == signalFrameVersion {
				frameBase, found = candidate, true
				break
			}
		}
	}
	if !found {
		return fmt.Errorf("rt_sigreturn: no valid signal frame at esp=0x%08x", esp)
	}
	read := func(offset uint32) (uint32, error) {
		return cpu.Mem.Read32(frameBase + offset)
	}
	oldMaskLo, err := read(32)
	if err != nil {
		return err
	}
	oldMaskHi, err := read(36)
	if err != nil {
		return err
	}
	oldEIP, err := read(20)
	if err != nil {
		return err
	}
	oldEFLAGS, err := read(24)
	if err != nil {
		return err
	}
	oldESP, err := read(28)
	if err != nil {
		return err
	}
	if oldESP == 0 {
		return fmt.Errorf("rt_sigreturn: invalid saved esp")
	}
	var regs [8]uint32
	for i := range regs {
		regs[i], err = read(72 + uint32(i*4))
		if err != nil {
			return err
		}
	}
	var fpu [8]float64
	for i := range fpu {
		lo, readErr := read(104 + uint32(i*8))
		if readErr != nil {
			return readErr
		}
		hi, readErr := read(108 + uint32(i*8))
		if readErr != nil {
			return readErr
		}
		fpu[i] = math.Float64frombits(uint64(lo) | uint64(hi)<<32)
	}
	fsBase, err := read(40)
	if err != nil {
		return err
	}
	gsBase, err := read(44)
	if err != nil {
		return err
	}
	fsSelector, err := read(48)
	if err != nil {
		return err
	}
	gsSelector, err := read(52)
	if err != nil {
		return err
	}
	fpuTop, err := read(56)
	if err != nil {
		return err
	}
	fpuCount, err := read(60)
	if err != nil {
		return err
	}
	fpuStatus, err := read(64)
	if err != nil {
		return err
	}
	fpuControl, err := read(68)
	if err != nil {
		return err
	}
	if fpuTop > 7 || fpuCount > 8 || fsSelector > 0xffff || gsSelector > 0xffff {
		return fmt.Errorf("rt_sigreturn: invalid saved CPU state")
	}

	cpu.Regs = regs
	cpu.EIP = oldEIP
	cpu.EFLAGS = oldEFLAGS
	cpu.Regs[i386.ESP] = oldESP
	cpu.FSBase = fsBase
	cpu.GSBase = gsBase
	cpu.FSSelector = uint16(fsSelector)
	cpu.GSSelector = uint16(gsSelector)
	cpu.FPUTop = uint8(fpuTop)
	cpu.FPUCount = uint8(fpuCount)
	cpu.FPUStatus = uint16(fpuStatus)
	cpu.FPUControl = uint16(fpuControl)
	cpu.FPU = fpu
	k.signalMask = uint64(oldMaskLo) | uint64(oldMaskHi)<<32
	cpu.Halted = false
	return nil
}
