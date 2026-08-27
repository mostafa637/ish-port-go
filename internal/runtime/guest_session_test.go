package runtime

import (
	"archive/tar"
	"bytes"
	"debug/elf"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func runtimeELF(code []byte) []byte {
	const header = 52
	const phSize = 32
	const offset = 0x100
	const vaddr = 0x1000
	data := make([]byte, offset+len(code))
	copy(data[:4], []byte{0x7f, 'E', 'L', 'F'})
	data[4], data[5], data[6] = byte(elf.ELFCLASS32), byte(elf.ELFDATA2LSB), 1
	binary.LittleEndian.PutUint16(data[16:], uint16(elf.ET_EXEC))
	binary.LittleEndian.PutUint16(data[18:], uint16(elf.EM_386))
	binary.LittleEndian.PutUint32(data[20:], 1)
	binary.LittleEndian.PutUint32(data[24:], vaddr)
	binary.LittleEndian.PutUint32(data[28:], header)
	binary.LittleEndian.PutUint16(data[40:], header)
	binary.LittleEndian.PutUint16(data[42:], phSize)
	binary.LittleEndian.PutUint16(data[44:], 1)
	ph := data[header : header+phSize]
	binary.LittleEndian.PutUint32(ph[0:], uint32(elf.PT_LOAD))
	binary.LittleEndian.PutUint32(ph[4:], offset)
	binary.LittleEndian.PutUint32(ph[8:], vaddr)
	binary.LittleEndian.PutUint32(ph[12:], vaddr)
	binary.LittleEndian.PutUint32(ph[16:], uint32(len(code)))
	binary.LittleEndian.PutUint32(ph[20:], uint32(len(code)))
	binary.LittleEndian.PutUint32(ph[24:], uint32(elf.PF_R|elf.PF_X))
	binary.LittleEndian.PutUint32(ph[28:], 0x1000)
	copy(data[offset:], code)
	return data
}

func TestSessionRunELFUsesSharedRuntime(t *testing.T) {
	root := t.TempDir()
	program := filepath.Join(root, "bin", "status")
	if err := os.MkdirAll(filepath.Dir(program), 0o700); err != nil {
		t.Fatal(err)
	}
	code := []byte{
		0xBB, 9, 0, 0, 0, // exit status
		0xB8, 1, 0, 0, 0, // SYS_exit
		0xCD, 0x80,
	}
	if err := os.WriteFile(program, runtimeELF(code), 0o700); err != nil {
		t.Fatal(err)
	}
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	codeOut, err := s.RunELF(t.Context(), program, []string{"/bin/status"}, 100)
	if err != nil || codeOut != 9 {
		t.Fatalf("RunELF=(%d,%v)", codeOut, err)
	}
	if s.Scheduler == nil || len(s.Scheduler.PIDs()) != 1 {
		t.Fatalf("scheduler state=%v", s.Scheduler)
	}
	if _, err := s.PTY.WriteInput([]byte("input\n")); err != nil {
		t.Fatal(err)
	}
	var buf [16]byte
	n, err := s.PTY.ReadInput(t.Context(), buf[:])
	if err != nil || string(buf[:n]) != "input\n" {
		t.Fatalf("pty=(%q,%v)", buf[:n], err)
	}
	_ = tar.TypeReg
	_ = bytes.MinRead
}
