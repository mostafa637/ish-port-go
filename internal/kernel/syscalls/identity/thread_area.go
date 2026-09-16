package identity

import "example.com/ish-go/internal/i386"

func SetThreadArea(addr uint32, cpu *i386.CPU) int32 {
	base, err := cpu.Mem.Read32(addr + 4)
	if err != nil {
		return -14
	}
	cpu.GSBase = base
	return 0
}
