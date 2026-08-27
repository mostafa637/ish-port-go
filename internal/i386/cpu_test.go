package i386

import (
	"math"
	"testing"
)

func runCode(t *testing.T, code []byte, setup func(*CPU)) *CPU {
	t.Helper()
	mem := NewMemory(4096)
	if err := mem.WriteBytes(0, code); err != nil {
		t.Fatal(err)
	}
	cpu := NewCPU(mem)
	cpu.Regs[ESP] = 2048
	if setup != nil {
		setup(cpu)
	}
	if err := cpu.Run(100); err != nil {
		t.Fatal(err)
	}
	return cpu
}

func TestMovAddAndHalt(t *testing.T) {
	cpu := runCode(t, []byte{
		0xB8, 2, 0, 0, 0, // mov eax, 2
		0x05, 3, 0, 0, 0, // add eax, 3
		0xF4,
	}, nil)
	if cpu.Regs[EAX] != 5 || !cpu.Halted {
		t.Fatalf("eax=%d halted=%t", cpu.Regs[EAX], cpu.Halted)
	}
}

func TestCallReturnAndStack(t *testing.T) {
	cpu := runCode(t, []byte{
		0xE8, 7, 0, 0, 0, // call 12
		0xB8, 7, 0, 0, 0, // mov eax, 7
		0xF4,
		0x90,             // padding at 11
		0xB8, 9, 0, 0, 0, // mov eax, 9
		0xC3,
	}, nil)
	if cpu.Regs[EAX] != 7 {
		t.Fatalf("eax=%d, return address was not restored", cpu.Regs[EAX])
	}
}

func TestConditionalJump(t *testing.T) {
	cpu := runCode(t, []byte{
		0xB8, 1, 0, 0, 0, // mov eax, 1
		0x3D, 1, 0, 0, 0, // cmp eax, 1
		0x74, 5, // je +5, skip mov eax, 0
		0xB8, 0, 0, 0, 0,
		0xF4,
	}, nil)
	if cpu.Regs[EAX] != 1 {
		t.Fatalf("conditional jump failed: eax=%d", cpu.Regs[EAX])
	}
}

func TestInt80Callback(t *testing.T) {
	called := false
	cpu := runCode(t, []byte{0xCD, 0x80, 0xF4}, func(cpu *CPU) {
		cpu.OnSyscall = func(cpu *CPU) error {
			called = true
			cpu.Regs[EAX] = 42
			return nil
		}
	})
	if !called || cpu.Regs[EAX] != 42 {
		t.Fatalf("syscall callback called=%t eax=%d", called, cpu.Regs[EAX])
	}
}

func TestSegmentOverrideUsesFSBaseAndResets(t *testing.T) {
	mem := NewMemory(4096)
	code := []byte{
		0x64, 0x8B, 0x05, 0x00, 0x00, 0x00, 0x00, // mov eax, fs:[0]
		0x8B, 0x1D, 0x00, 0x03, 0x00, 0x00, // mov ebx, [0x300] (no stale FS)
		0xF4,
	}
	if err := mem.WriteBytes(0, code); err != nil {
		t.Fatal(err)
	}
	if err := mem.Write32(0x200, 0x11223344); err != nil {
		t.Fatal(err)
	}
	if err := mem.Write32(0x300, 7); err != nil {
		t.Fatal(err)
	}
	cpu := NewCPU(mem)
	cpu.Regs[ESP] = 2048
	cpu.FSBase = 0x200
	if err := cpu.Run(20); err != nil {
		t.Fatal(err)
	}
	if cpu.Regs[EAX] != 0x11223344 || cpu.Regs[EBX] != 7 {
		t.Fatalf("eax=0x%x ebx=%d", cpu.Regs[EAX], cpu.Regs[EBX])
	}
}

func TestSegmentOverrideUsesGSBase(t *testing.T) {
	mem := NewMemory(4096)
	if err := mem.WriteBytes(0, []byte{0x65, 0x8B, 0x05, 0, 0, 0, 0, 0xF4}); err != nil {
		t.Fatal(err)
	}
	if err := mem.Write32(0x240, 0x55667788); err != nil {
		t.Fatal(err)
	}
	cpu := NewCPU(mem)
	cpu.GSBase = 0x240
	if err := cpu.Run(10); err != nil {
		t.Fatal(err)
	}
	if cpu.Regs[EAX] != 0x55667788 {
		t.Fatalf("eax=0x%x", cpu.Regs[EAX])
	}
}

func TestSubAndCmpRegDirection(t *testing.T) {
	cpu := runCode(t, []byte{
		0xB8, 3, 0, 0, 0, // mov eax, 3
		0xBB, 5, 0, 0, 0, // mov ebx, 5
		0x3B, 0xC3, // cmp eax, ebx => CF for 3 < 5
		0x2B, 0xC3, // sub eax, ebx => -2
		0xF4,
	}, nil)
	want := uint32(^uint32(1))
	if cpu.Regs[EAX] != want {
		t.Fatalf("eax=0x%x", cpu.Regs[EAX])
	}
	if cpu.EFLAGS&FlagCF == 0 {
		t.Fatalf("cmp did not set CF: flags=0x%x", cpu.EFLAGS)
	}
}

func TestCmpByteMemoryUsesByteOperand(t *testing.T) {
	mem := NewMemory(4096)
	code := []byte{
		0xB8, 0x5F, 0x00, 0x00, 0x00, // mov eax, '_'
		0x3A, 0x05, 0x00, 0x02, 0x00, 0x00, // cmp al, byte ptr [0x200]
		0x74, 0x06, // je equal
		0xB8, 0x01, 0x00, 0x00, 0x00, // failure marker
		0xF4,
		0xB8, 0x2A, 0x00, 0x00, 0x00, // equal marker
		0xF4,
	}
	if err := mem.WriteBytes(0, code); err != nil {
		t.Fatal(err)
	}
	if err := mem.WriteBytes(0x200, []byte{'_', 'x', 'x', 'x'}); err != nil {
		t.Fatal(err)
	}
	cpu := NewCPU(mem)
	if err := cpu.Run(30); err != nil {
		t.Fatal(err)
	}
	if cpu.Regs[EAX] != 0x2a {
		t.Fatalf("cmp byte did not compare equal: eax=0x%x flags=0x%x", cpu.Regs[EAX], cpu.EFLAGS)
	}
}

func TestRepMovsd(t *testing.T) {
	mem := NewMemory(4096)
	if err := mem.WriteBytes(0, []byte{0xFC, 0xF3, 0xA5, 0xF4}); err != nil {
		t.Fatal(err)
	}
	if err := mem.Write32(0x200, 0x11223344); err != nil {
		t.Fatal(err)
	}
	if err := mem.Write32(0x204, 0x55667788); err != nil {
		t.Fatal(err)
	}
	cpu := NewCPU(mem)
	cpu.Regs[ESI] = 0x200
	cpu.Regs[EDI] = 0x300
	cpu.Regs[ECX] = 2
	if err := cpu.Run(10); err != nil {
		t.Fatal(err)
	}
	first, _ := mem.Read32(0x300)
	second, _ := mem.Read32(0x304)
	if first != 0x11223344 || second != 0x55667788 || cpu.Regs[ECX] != 0 || cpu.Regs[ESI] != 0x208 || cpu.Regs[EDI] != 0x308 {
		t.Fatalf("rep movsd: first=0x%x second=0x%x ecx=0x%x esi=0x%x edi=0x%x", first, second, cpu.Regs[ECX], cpu.Regs[ESI], cpu.Regs[EDI])
	}
}

func TestCmpxchg32(t *testing.T) {
	cpu := runCode(t, []byte{
		0xB8, 5, 0, 0, 0, // mov eax, 5
		0xBB, 9, 0, 0, 0, // mov ebx, 9
		0x0F, 0xB1, 0x1D, 0x00, 0x02, 0x00, 0x00, // cmpxchg [0x200], ebx
		0xF4,
	}, func(cpu *CPU) {
		if err := cpu.Mem.Write32(0x200, 5); err != nil {
			t.Fatal(err)
		}
	})
	value, _ := cpu.Mem.Read32(0x200)
	if value != 9 || cpu.EFLAGS&FlagZF == 0 {
		t.Fatalf("cmpxchg value=0x%x flags=0x%x", value, cpu.EFLAGS)
	}
}

func TestBitScanSemantics(t *testing.T) {
	tests := []struct {
		name    string
		opcode  byte
		source  uint32
		initial uint32
		want    uint32
		zero    bool
	}{
		{name: "bsf-low", opcode: 0xBC, source: 0x10, want: 4},
		{name: "bsr-small", opcode: 0xBD, source: 0x1e, want: 4},
		{name: "bsr-high", opcode: 0xBD, source: 0x80000000, want: 31},
		{name: "bsf-zero", opcode: 0xBC, source: 0, initial: 0x12345678, want: 0x12345678, zero: true},
		{name: "bsr-zero", opcode: 0xBD, source: 0, initial: 0x87654321, want: 0x87654321, zero: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code := []byte{
				0xB8, byte(tc.source), byte(tc.source >> 8), byte(tc.source >> 16), byte(tc.source >> 24), // mov eax, source
				0xBB, byte(tc.initial), byte(tc.initial >> 8), byte(tc.initial >> 16), byte(tc.initial >> 24), // mov ebx, initial
				0x0F, tc.opcode, 0xD8, // bsf/bsr ebx, eax
				0xF4,
			}
			cpu := runCode(t, code, nil)
			if cpu.Regs[EBX] != tc.want {
				t.Fatalf("ebx=0x%x want=0x%x", cpu.Regs[EBX], tc.want)
			}
			if tc.zero != (cpu.EFLAGS&FlagZF != 0) {
				t.Fatalf("ZF=%t want=%t flags=0x%x", cpu.EFLAGS&FlagZF != 0, tc.zero, cpu.EFLAGS)
			}
		})
	}
}

func TestCDQSignExtension(t *testing.T) {
	tests := []struct {
		name string
		eax  uint32
		edx  uint32
	}{
		{name: "positive", eax: 0x7fffffff, edx: 0},
		{name: "negative", eax: 0x80000000, edx: 0xffffffff},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code := []byte{0xB8, byte(tc.eax), byte(tc.eax >> 8), byte(tc.eax >> 16), byte(tc.eax >> 24), 0x99, 0xF4}
			cpu := runCode(t, code, nil)
			if cpu.Regs[EDX] != tc.edx {
				t.Fatalf("edx=0x%x want=0x%x", cpu.Regs[EDX], tc.edx)
			}
		})
	}
}

func TestRepStosb(t *testing.T) {
	cpu := runCode(t, []byte{0xF3, 0xAA, 0xF4}, func(cpu *CPU) {
		cpu.Regs[EAX] = 0x1122337f
		cpu.Regs[EDI] = 0x200
		cpu.Regs[ECX] = 3
	})
	for addr := uint32(0x200); addr < 0x203; addr++ {
		value, err := cpu.Mem.Read8(addr)
		if err != nil || value != 0x7f {
			t.Fatalf("byte[0x%x]=(0x%x,%v)", addr, value, err)
		}
	}
	if cpu.Regs[EDI] != 0x203 || cpu.Regs[ECX] != 0 {
		t.Fatalf("rep stosb edi=0x%x ecx=%d", cpu.Regs[EDI], cpu.Regs[ECX])
	}
}

func TestBitImmediateGroup(t *testing.T) {
	tests := []struct {
		name    string
		group   byte
		bit     byte
		wantEAX uint32
		wantCF  bool
	}{
		{name: "bt", group: 4, bit: 4, wantEAX: 0x10, wantCF: true},
		{name: "bts", group: 5, bit: 1, wantEAX: 0x12, wantCF: false},
		{name: "btr", group: 6, bit: 4, wantEAX: 0, wantCF: true},
		{name: "btc", group: 7, bit: 4, wantEAX: 0, wantCF: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			modrm := byte(0xC0 | (tc.group << 3))
			code := []byte{
				0xB8, 0x10, 0, 0, 0, // mov eax, 0x10
				0x0F, 0xBA, modrm, tc.bit,
				0xF4,
			}
			cpu := runCode(t, code, nil)
			if cpu.Regs[EAX] != tc.wantEAX {
				t.Fatalf("eax=0x%x want=0x%x", cpu.Regs[EAX], tc.wantEAX)
			}
			if got := cpu.EFLAGS&FlagCF != 0; got != tc.wantCF {
				t.Fatalf("CF=%t want=%t flags=0x%x", got, tc.wantCF, cpu.EFLAGS)
			}
		})
	}
}

func TestXorByteRegisterOperand(t *testing.T) {
	cpu := runCode(t, []byte{
		0xB8, 0xf0, 0, 0, 0, // mov eax, 0xf0
		0xB3, 0x0f, // mov bl, 0x0f
		0x30, 0xd8, // xor al, bl
		0xF4,
	}, nil)
	if cpu.byteReg(0) != 0xff {
		t.Fatalf("al=0x%x", cpu.byteReg(0))
	}
	if cpu.EFLAGS&FlagZF != 0 {
		t.Fatalf("xor incorrectly set ZF: flags=0x%x", cpu.EFLAGS)
	}
}

func TestAdcRegisterForms(t *testing.T) {
	t.Run("dword-with-carry", func(t *testing.T) {
		cpu := runCode(t, []byte{
			0xB8, 0, 0, 0, 0, // mov eax, 0
			0xBB, 1, 0, 0, 0, // mov ebx, 1
			0x3B, 0xC3, // cmp eax, ebx => CF=1
			0x13, 0xC3, // adc eax, ebx
			0xF4,
		}, nil)
		if cpu.Regs[EAX] != 2 {
			t.Fatalf("eax=0x%x want=0x2", cpu.Regs[EAX])
		}
	})

	t.Run("byte", func(t *testing.T) {
		cpu := runCode(t, []byte{
			0xB8, 1, 0, 0, 0, // mov eax, 1
			0xB3, 2, // mov bl, 2
			0x10, 0xD8, // adc al, bl
			0xF4,
		}, nil)
		if cpu.byteReg(0) != 3 {
			t.Fatalf("al=0x%x want=0x3", cpu.byteReg(0))
		}
	})
}

func TestOrByteRegisterForms(t *testing.T) {
	t.Run("rm8-r8", func(t *testing.T) {
		cpu := runCode(t, []byte{
			0xB8, 0x10, 0, 0, 0, // mov eax, 0x10
			0xB3, 0x03, // mov bl, 3
			0x08, 0xD8, // or al, bl
			0xF4,
		}, nil)
		if cpu.byteReg(0) != 0x13 {
			t.Fatalf("al=0x%x want=0x13", cpu.byteReg(0))
		}
	})

	t.Run("r8-rm8", func(t *testing.T) {
		cpu := runCode(t, []byte{
			0xB8, 0x10, 0, 0, 0, // mov eax, 0x10
			0xB3, 0x03, // mov bl, 3
			0x0A, 0xC3, // or al, bl
			0xF4,
		}, nil)
		if cpu.byteReg(0) != 0x13 {
			t.Fatalf("al=0x%x want=0x13", cpu.byteReg(0))
		}
	})
}

func TestShiftByteByOne(t *testing.T) {
	tests := []struct {
		name   string
		group  byte
		source byte
		want   byte
		wantCF bool
	}{
		{name: "shl", group: 4, source: 0x81, want: 0x02, wantCF: true},
		{name: "shr", group: 5, source: 0x81, want: 0x40, wantCF: true},
		{name: "sar", group: 7, source: 0x81, want: 0xc0, wantCF: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code := []byte{0xB0, tc.source, 0xD0, byte(0xC0 | tc.group<<3), 0xF4}
			cpu := runCode(t, code, nil)
			if got := cpu.byteReg(0); got != tc.want {
				t.Fatalf("al=0x%x want=0x%x", got, tc.want)
			}
			if got := cpu.EFLAGS&FlagCF != 0; got != tc.wantCF {
				t.Fatalf("CF=%t want=%t flags=0x%x", got, tc.wantCF, cpu.EFLAGS)
			}
		})
	}
}

func TestIncDecBytePreserveCarry(t *testing.T) {
	tests := []struct {
		name  string
		group byte
		start byte
		want  byte
	}{
		{name: "inc", group: 0, start: 0x7f, want: 0x80},
		{name: "dec", group: 1, start: 0x80, want: 0x7f},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code := []byte{0xB0, tc.start, 0xFE, byte(0xC0 | tc.group<<3), 0xF4}
			cpu := runCode(t, code, func(cpu *CPU) { cpu.EFLAGS |= FlagCF })
			if got := cpu.byteReg(0); got != tc.want {
				t.Fatalf("al=0x%x want=0x%x", got, tc.want)
			}
			if cpu.EFLAGS&FlagCF == 0 {
				t.Fatalf("INC/DEC changed CF: flags=0x%x", cpu.EFLAGS)
			}
		})
	}
}

func TestAndByteRegisterForms(t *testing.T) {
	t.Run("rm8-r8", func(t *testing.T) {
		cpu := runCode(t, []byte{
			0xB8, 0xf3, 0, 0, 0, // mov eax, 0xf3
			0xB3, 0x0f, // mov bl, 0x0f
			0x20, 0xd8, // and al, bl
			0xF4,
		}, nil)
		if cpu.byteReg(0) != 0x03 {
			t.Fatalf("al=0x%x want=0x3", cpu.byteReg(0))
		}
	})

	t.Run("r8-rm8", func(t *testing.T) {
		cpu := runCode(t, []byte{
			0xB8, 0xf3, 0, 0, 0, // mov eax, 0xf3
			0xB3, 0x0f, // mov bl, 0x0f
			0x22, 0xc3, // and al, bl
			0xF4,
		}, nil)
		if cpu.byteReg(0) != 0x03 {
			t.Fatalf("al=0x%x want=0x3", cpu.byteReg(0))
		}
	})
}

func TestAdcAccumulatorForms(t *testing.T) {
	t.Run("al-immediate-with-carry", func(t *testing.T) {
		cpu := runCode(t, []byte{
			0xB0, 1,
			0x3C, 2, // cmp al, 2 => CF=1
			0x14, 3, // adc al, 3
			0xF4,
		}, nil)
		if cpu.byteReg(0) != 5 {
			t.Fatalf("al=0x%x want=0x5", cpu.byteReg(0))
		}
	})

	t.Run("eax-immediate", func(t *testing.T) {
		cpu := runCode(t, []byte{
			0xB8, 1, 0, 0, 0,
			0x15, 2, 0, 0, 0, // adc eax, 2
			0xF4,
		}, nil)
		if cpu.Regs[EAX] != 3 {
			t.Fatalf("eax=0x%x want=0x3", cpu.Regs[EAX])
		}
	})
}

func TestShldImmediate(t *testing.T) {
	cpu := runCode(t, []byte{
		0xB8, 1, 0, 0, 0, // mov eax, 1
		0xBB, 0, 0, 0x00, 0x80, // mov ebx, 0x80000000
		0x0F, 0xA4, 0xD8, 1, // shld eax, ebx, 1
		0xF4,
	}, nil)
	if cpu.Regs[EAX] != 3 {
		t.Fatalf("eax=0x%x want=0x3", cpu.Regs[EAX])
	}
	if cpu.EFLAGS&FlagCF != 0 {
		t.Fatalf("unexpected CF: flags=0x%x", cpu.EFLAGS)
	}
}

func TestOrAccumulatorImmediate(t *testing.T) {
	cpu := runCode(t, []byte{
		0xB0, 0x10,
		0x0C, 0x03, // or al, 3
		0x0D, 0x00, 0x00, 0x00, 0x00, // or eax, 0
		0xF4,
	}, nil)
	if cpu.Regs[EAX]&0xff != 0x13 {
		t.Fatalf("al=0x%x want=0x13", cpu.Regs[EAX]&0xff)
	}
}

func TestAndAccumulatorImmediate(t *testing.T) {
	cpu := runCode(t, []byte{
		0xB0, 0xf3,
		0x24, 0x0f, // and al, 0x0f
		0x25, 0xff, 0xff, 0xff, 0xff, // and eax, -1
		0xF4,
	}, nil)
	if cpu.Regs[EAX]&0xff != 0x03 {
		t.Fatalf("al=0x%x want=0x3", cpu.Regs[EAX]&0xff)
	}
}

func TestAddRegisterAndAccumulatorForms(t *testing.T) {
	t.Run("byte-register", func(t *testing.T) {
		cpu := runCode(t, []byte{
			0xB0, 1,
			0xB3, 2,
			0x02, 0xC3, // add al, bl
			0xF4,
		}, nil)
		if cpu.byteReg(0) != 3 {
			t.Fatalf("al=0x%x want=0x3", cpu.byteReg(0))
		}
	})

	t.Run("accumulator", func(t *testing.T) {
		cpu := runCode(t, []byte{
			0xB8, 1, 0, 0, 0,
			0x05, 2, 0, 0, 0, // add eax, 2
			0xF4,
		}, nil)
		if cpu.Regs[EAX] != 3 {
			t.Fatalf("eax=0x%x want=0x3", cpu.Regs[EAX])
		}
	})
}

func TestControlFlagInstructions(t *testing.T) {
	tests := []struct {
		name string
		code []byte
		mask uint32
		want bool
	}{
		{name: "clc", code: []byte{0xF8, 0xF4}, mask: FlagCF, want: false},
		{name: "stc", code: []byte{0xF9, 0xF4}, mask: FlagCF, want: true},
		{name: "cli", code: []byte{0xFA, 0xF4}, mask: FlagIF, want: false},
		{name: "sti", code: []byte{0xFB, 0xF4}, mask: FlagIF, want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu := runCode(t, tc.code, func(cpu *CPU) { cpu.EFLAGS |= tc.mask })
			if got := cpu.EFLAGS&tc.mask != 0; got != tc.want {
				t.Fatalf("flags=0x%x mask=0x%x got=%t want=%t", cpu.EFLAGS, tc.mask, got, tc.want)
			}
		})
	}
}

func TestMovRM16ImmediateConsumes16Bits(t *testing.T) {
	cpu := runCode(t, []byte{
		0xB8, 0, 0, 0, 0, // mov eax, 0
		0x66, 0xC7, 0xC0, 0x00, 0x01, // mov ax, 0x100
		0x3D, 0x00, 0x01, 0x00, 0x00, // cmp eax, 0x100
		0x74, 0x02, // je over failure marker
		0xB0, 0xff,
		0xF4,
	}, nil)
	if cpu.Regs[EAX] != 0x100 {
		t.Fatalf("eax=0x%x want=0x100", cpu.Regs[EAX])
	}
}

func TestMemoryBitRegisterFormsCrossWord(t *testing.T) {
	mem := NewMemory(4096)
	code := []byte{
		0xB9, 0x20, 0x00, 0x00, 0x00, // ecx = 32
		0x0F, 0xA3, 0x0D, 0x00, 0x02, 0x00, 0x00, // bt [0x200], ecx
		0x0F, 0xAB, 0x0D, 0x00, 0x02, 0x00, 0x00, // bts [0x200], ecx
		0xF4,
	}
	if err := mem.WriteBytes(0, code); err != nil {
		t.Fatal(err)
	}
	if err := mem.Write32(0x204, 1); err != nil {
		t.Fatal(err)
	}
	cpu := NewCPU(mem)
	if err := cpu.Run(20); err != nil {
		t.Fatal(err)
	}
	if cpu.EFLAGS&FlagCF == 0 {
		t.Fatal("bt bit 32 did not set CF")
	}
	word, err := mem.Read32(0x204)
	if err != nil || word != 1 {
		t.Fatalf("bts word=0x%x err=%v", word, err)
	}
}

func TestMovOperand16RegisterAndMemoryForms(t *testing.T) {
	mem := NewMemory(4096)
	code := []byte{
		0xB8, 0x78, 0x56, 0x34, 0x12, // mov eax, 0x12345678
		0xBB, 0xef, 0xcd, 0xab, 0x89, // mov ebx, 0x89abcdef
		0x66, 0x89, 0xd8, // mov ax, bx
		0x66, 0x89, 0x1d, 0x00, 0x02, 0x00, 0x00, // mov [0x200], bx
		0xB9, 0x00, 0x00, 0x00, 0x00, // mov ecx, 0
		0x66, 0x8b, 0x0d, 0x00, 0x02, 0x00, 0x00, // mov cx, [0x200]
		0xF4,
	}
	if err := mem.WriteBytes(0, code); err != nil {
		t.Fatal(err)
	}
	cpu := NewCPU(mem)
	if err := cpu.Run(100); err != nil {
		t.Fatal(err)
	}
	if got, want := cpu.Regs[EAX], uint32(0x1234cdef); got != want {
		t.Fatalf("eax=0x%x want=0x%x", got, want)
	}
	if got, err := mem.Read16(0x200); err != nil || got != 0xcdef {
		t.Fatalf("memory word=0x%x err=%v want=0xcdef", got, err)
	}
	if got, want := cpu.Regs[ECX], uint32(0xcdef); got != want {
		t.Fatalf("ecx=0x%x want=0x%x", got, want)
	}
}

func TestShiftOperand16PreservesUpperRegister(t *testing.T) {
	cpu := runCode(t, []byte{
		0xB8, 0x00, 0x80, 0x34, 0x12, // mov eax, 0x12348000
		0x66, 0xC1, 0xE8, 0x08, // shr ax, 8
		0xF4,
	}, nil)
	if got, want := cpu.Regs[EAX], uint32(0x12340080); got != want {
		t.Fatalf("eax=0x%x want=0x%x", got, want)
	}
}

func TestAccumulatorLogicOperand16Consumes16Bits(t *testing.T) {
	cpu := runCode(t, []byte{
		0xB8, 0x34, 0x12, 0x78, 0x56, // mov eax, 0x56781234
		0x66, 0x25, 0x00, 0xf0, // and ax, 0xf000
		0xF4,
	}, nil)
	if got, want := cpu.Regs[EAX], uint32(0x56781000); got != want {
		t.Fatalf("eax=0x%x want=0x%x", got, want)
	}
}

func TestX87BasicStackAndMemoryOperations(t *testing.T) {
	mem := NewMemory(4096)
	value := math.Float64bits(2.5)
	if err := mem.WriteBytes(0x300, []byte{
		byte(value), byte(value >> 8), byte(value >> 16), byte(value >> 24),
		byte(value >> 32), byte(value >> 40), byte(value >> 48), byte(value >> 56),
	}); err != nil {
		t.Fatal(err)
	}
	code := []byte{
		0xDD, 0x05, 0x00, 0x03, 0x00, 0x00, // fld qword [0x300]
		0xD9, 0xEE, // fldz
		0xDE, 0xC1, // faddp st(1), st(0)
		0xDD, 0x5C, 0x24, 0x08, // fstp qword [esp+8]
		0xF4,
	}
	if err := mem.WriteBytes(0, code); err != nil {
		t.Fatal(err)
	}
	cpu := NewCPU(mem)
	cpu.Regs[ESP] = 2048
	if err := cpu.Run(100); err != nil {
		t.Fatal(err)
	}
	data, err := mem.ReadBytes(cpu.Regs[ESP]+8, 8)
	if err != nil {
		t.Fatal(err)
	}
	got := uint64(data[0]) | uint64(data[1])<<8 | uint64(data[2])<<16 | uint64(data[3])<<24 |
		uint64(data[4])<<32 | uint64(data[5])<<40 | uint64(data[6])<<48 | uint64(data[7])<<56
	if math.Float64frombits(got) != 2.5 {
		t.Fatalf("x87 result=%v want=2.5", math.Float64frombits(got))
	}
}

func TestBSWAPRegisterForms(t *testing.T) {
	mem := NewMemory(64)
	cpu := NewCPU(mem)
	cpu.Regs[EAX] = 0x11223344
	cpu.Regs[EDX] = 0xaabbccdd
	cpu.EFLAGS = FlagCF | FlagZF
	if err := mem.WriteBytes(0, []byte{0x0f, 0xc8, 0x0f, 0xca}); err != nil {
		t.Fatal(err)
	}
	if err := cpu.Step(); err != nil {
		t.Fatal(err)
	}
	if cpu.Regs[EAX] != 0x44332211 || cpu.EFLAGS != FlagCF|FlagZF {
		t.Fatalf("bswap eax=0x%x eflags=0x%x", cpu.Regs[EAX], cpu.EFLAGS)
	}
	if err := cpu.Step(); err != nil {
		t.Fatal(err)
	}
	if cpu.Regs[EDX] != 0xddccbbaa || cpu.EFLAGS != FlagCF|FlagZF {
		t.Fatalf("bswap edx=0x%x eflags=0x%x", cpu.Regs[EDX], cpu.EFLAGS)
	}
}
