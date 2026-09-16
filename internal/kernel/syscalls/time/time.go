package time

import (
	"encoding/binary"
	"example.com/ish-go/internal/i386"
	stdtime "time"
)

func Gettimeofday(addr uint32, cpu *i386.CPU) bool {
	now := stdtime.Now()
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b, uint32(now.Unix()))
	binary.LittleEndian.PutUint32(b[4:], uint32(now.Nanosecond()/1000))
	return cpu.Mem.WriteBytes(addr, b) == nil
}
