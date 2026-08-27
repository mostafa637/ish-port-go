package elf32

import (
	"encoding/binary"
	"testing"

	"example.com/ish-go/internal/i386"
)

func TestApplyRELRDirectAndBitmap(t *testing.T) {
	space := i386.NewAddressSpace(0x2000)
	if err := space.Add(0x1100, 0x100, i386.ProtRead|i386.ProtWrite, "reloc"); err != nil {
		t.Fatal(err)
	}
	if err := space.Mem.Write32(0x1100, 5); err != nil {
		t.Fatal(err)
	}
	if err := space.Mem.Write32(0x1104, 7); err != nil {
		t.Fatal(err)
	}
	if err := space.Mem.Write32(0x110c, 11); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 8)
	binary.LittleEndian.PutUint32(data[0:], 0x100)
	// The bitmap starts at the word after the direct relocation. Bits 1 and 3
	// relocate offsets +0 and +8 from that cursor.
	binary.LittleEndian.PutUint32(data[4:], (1<<1)|(1<<3)|1)
	if err := applyRELR(space, data, 0x1000); err != nil {
		t.Fatal(err)
	}
	for addr, want := range map[uint32]uint32{0x1100: 0x1005, 0x1104: 0x1007, 0x110c: 0x100b} {
		got, err := space.Mem.Read32(addr)
		if err != nil || got != want {
			t.Fatalf("relr[0x%x]=0x%x err=%v want=0x%x", addr, got, err, want)
		}
	}
}
