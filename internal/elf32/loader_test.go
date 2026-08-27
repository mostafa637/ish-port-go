package elf32

import (
	"encoding/binary"
	"testing"

	"debug/elf"
)

func tinyELF(code []byte) []byte {
	const (
		headerSize = 52
		phSize     = 32
		off        = 0x100
		vaddr      = 0x1000
	)
	data := make([]byte, off+len(code))
	copy(data[0:4], []byte{0x7f, 'E', 'L', 'F'})
	data[4] = byte(elf.ELFCLASS32)
	data[5] = byte(elf.ELFDATA2LSB)
	data[6] = 1
	binary.LittleEndian.PutUint16(data[16:], uint16(elf.ET_EXEC))
	binary.LittleEndian.PutUint16(data[18:], uint16(elf.EM_386))
	binary.LittleEndian.PutUint32(data[20:], 1)
	binary.LittleEndian.PutUint32(data[24:], vaddr)
	binary.LittleEndian.PutUint32(data[28:], headerSize)
	binary.LittleEndian.PutUint32(data[32:], 0)
	binary.LittleEndian.PutUint32(data[36:], 0)
	binary.LittleEndian.PutUint16(data[40:], headerSize)
	binary.LittleEndian.PutUint16(data[42:], phSize)
	binary.LittleEndian.PutUint16(data[44:], 1)
	ph := data[headerSize : headerSize+phSize]
	binary.LittleEndian.PutUint32(ph[0:], uint32(elf.PT_LOAD))
	binary.LittleEndian.PutUint32(ph[4:], off)
	binary.LittleEndian.PutUint32(ph[8:], vaddr)
	binary.LittleEndian.PutUint32(ph[12:], vaddr)
	binary.LittleEndian.PutUint32(ph[16:], uint32(len(code)))
	binary.LittleEndian.PutUint32(ph[20:], uint32(len(code)))
	binary.LittleEndian.PutUint32(ph[24:], uint32(elf.PF_R|elf.PF_X))
	binary.LittleEndian.PutUint32(ph[28:], 0x1000)
	copy(data[off:], code)
	return data
}

func TestLoadAndRunTinyELF(t *testing.T) {
	image, err := Load(tinyELF([]byte{0xB8, 7, 0, 0, 0, 0xF4}), []string{"hello"})
	if err != nil {
		t.Fatal(err)
	}
	if image.Entry != 0x1000 || image.CPU.Regs[4] == 0 {
		t.Fatalf("entry=0x%x stack=0x%x", image.Entry, image.CPU.Regs[4])
	}
	if err := image.CPU.Run(10); err != nil {
		t.Fatal(err)
	}
	if image.CPU.Regs[0] != 7 || !image.CPU.Halted {
		t.Fatalf("eax=%d halted=%t", image.CPU.Regs[0], image.CPU.Halted)
	}
}
