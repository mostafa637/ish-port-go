package system

import "example.com/ish-go/internal/i386"

func Uname(addr uint32, cpu *i386.CPU) bool {
	b := make([]byte, 390)
	for i, field := range []string{"Linux", "ish-go", "6.1.0-go", "#1", "i386", ""} {
		copy(b[i*65:i*65+65], field)
	}
	return cpu.Mem.WriteBytes(addr, b) == nil
}
