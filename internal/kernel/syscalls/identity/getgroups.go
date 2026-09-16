package identity

import "example.com/ish-go/internal/i386"

func GetGroups(size, addr uint32, cpu *i386.CPU) int32 {
	if size == 0 {
		return 1
	}
	if size < 1 {
		return -22
	}
	if cpu.Mem.Write32(addr, 0) != nil {
		return -14
	}
	return 1
}
