package system

import (
	"encoding/binary"
	"example.com/ish-go/internal/i386"
	"time"
)

func Sysinfo(addr uint32, start time.Time, cpu *i386.CPU) bool {
	b := make([]byte, 64)
	binary.LittleEndian.PutUint32(b, uint32(time.Since(start).Seconds()))
	mem := cpu.Mem.Size()
	binary.LittleEndian.PutUint32(b[16:], mem)
	binary.LittleEndian.PutUint32(b[20:], mem/2)
	binary.LittleEndian.PutUint16(b[40:], 1)
	binary.LittleEndian.PutUint32(b[52:], 1)
	return cpu.Mem.WriteBytes(addr, b) == nil
}
