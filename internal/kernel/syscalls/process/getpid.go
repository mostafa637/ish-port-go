package process

import "example.com/ish-go/internal/i386"

// GetPID returns the process or thread identifier.
func GetPID(pid int32, cpu *i386.CPU) {
	cpu.Regs[i386.EAX] = uint32(pid)
}
