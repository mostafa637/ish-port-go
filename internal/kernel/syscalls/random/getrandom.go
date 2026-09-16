package random

import (
	cryptorand "crypto/rand"
	"example.com/ish-go/internal/i386"
)

func Getrandom(addr, length uint32, cpu *i386.CPU) (int, bool) {
	if length > 1<<20 {
		length = 1 << 20
	}
	data := make([]byte, length)
	if _, err := cryptorand.Read(data); err != nil || cpu.Mem.WriteBytes(addr, data) != nil {
		return 0, false
	}
	return int(length), true
}
