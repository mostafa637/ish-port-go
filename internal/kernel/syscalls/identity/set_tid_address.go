package identity

import "example.com/ish-go/internal/i386"

// SetTIDAddress stores the address and returns the current task id.
func SetTIDAddress(addr *uint32, value uint32, pid int32, cpu *i386.CPU) {
	*addr = value
	cpu.Regs[i386.EAX] = uint32(pid)
}
