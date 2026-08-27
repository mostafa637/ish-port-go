package i386

import "testing"

func TestModRMMemoryArithmetic(t *testing.T) {
	// mov eax, 5; mov [0x300], eax; mov ebx, [0x300]; add ebx, eax; hlt
	code := []byte{
		0xB8, 5, 0, 0, 0,
		0x89, 0x05, 0x00, 0x03, 0x00, 0x00,
		0x8B, 0x1D, 0x00, 0x03, 0x00, 0x00,
		0x01, 0xC3,
		0xF4,
	}
	cpu := runCode(t, code, nil)
	if cpu.Regs[EBX] != 10 {
		t.Fatalf("ebx=%d, want 10", cpu.Regs[EBX])
	}
	value, err := cpu.Mem.Read32(0x300)
	if err != nil || value != 5 {
		t.Fatalf("memory=0x%x, err=%v", value, err)
	}
}

func TestModRMMemoryFault(t *testing.T) {
	mem := NewMemory(64)
	cpu := NewCPU(mem)
	cpu.Regs[ESP] = 32
	if err := mem.WriteBytes(0, []byte{0x89, 0x05, 0xFF, 0xFF, 0xFF, 0xFF}); err != nil {
		t.Fatal(err)
	}
	if err := cpu.Step(); err == nil {
		t.Fatal("expected memory fault")
	}
}
