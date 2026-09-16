package identity

import "example.com/ish-go/internal/i386"

// GetID returns the sandbox identity used by the guest.
func GetID(cpu *i386.CPU) { cpu.Regs[i386.EAX] = 0 }
