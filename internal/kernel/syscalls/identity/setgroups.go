package identity

import "example.com/ish-go/internal/i386"

func SetGroups(size, addr uint32, cpu *i386.CPU) int32 {
	if size > 1<<16 {
		return -22
	}
	if size > 0 {
		if _, err := cpu.Mem.ReadBytes(addr, size*4); err != nil {
			return -14
		}
	}
	return 0
}
