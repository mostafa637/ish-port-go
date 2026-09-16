package process

import "example.com/ish-go/internal/i386"

// SetPGID completes the supported process-group operation.
func SetPGID(cpu *i386.CPU) { cpu.Regs[i386.EAX] = 0 }

// SetSID returns the new session identifier.
func SetSID(pid int32, cpu *i386.CPU) { cpu.Regs[i386.EAX] = uint32(pid) }
