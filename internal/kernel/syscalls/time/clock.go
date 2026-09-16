package time

import (
	"example.com/ish-go/internal/i386"
	stdtime "time"
)

func ClockGettime(id, addr uint32, start stdtime.Time, cpu *i386.CPU) bool {
	if addr == 0 {
		return false
	}
	return cpu.Mem.WriteBytes(addr, clockData(id, start)) == nil
}
