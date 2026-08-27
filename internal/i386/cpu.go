package i386

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/bits"
)

const (
	EAX uint8 = iota
	ECX
	EDX
	EBX
	ESP
	EBP
	ESI
	EDI
)

const (
	FlagCF uint32 = 1 << 0
	FlagPF uint32 = 1 << 2
	FlagZF uint32 = 1 << 6
	FlagSF uint32 = 1 << 7
	FlagOF uint32 = 1 << 11
	FlagDF uint32 = 1 << 10
	FlagIF uint32 = 1 << 9
)

// SyscallHandler receives Linux i386 INT 0x80 traps. The arguments are read
// from the guest stack/registers by the kernel layer.
type SyscallHandler func(*CPU) error

// CPU is a small, deterministic i386 interpreter. It deliberately starts with
// the instruction families needed by the ELF/bootstrap tests; unsupported
// opcodes return a typed error instead of silently corrupting state.
type CPU struct {
	Regs        [8]uint32
	EIP         uint32
	EFLAGS      uint32
	Mem         *Memory
	Halted      bool
	Steps       uint64
	OnSyscall   SyscallHandler
	FSBase      uint32
	GSBase      uint32
	FSSelector  uint16
	GSSelector  uint16
	segmentBase uint32
	LastEIP     uint32
	LastOpcode  byte
	FPU         [8]float64
	FPUTop      uint8
	FPUCount    uint8
	FPUStatus   uint16
	FPUControl  uint16
}

func NewCPU(mem *Memory) *CPU {
	return &CPU{Mem: mem, EFLAGS: 2, FPUControl: 0x037f}
}

func (c *CPU) Reg(reg uint8) uint32       { return c.Regs[reg&7] }
func (c *CPU) SetReg(reg uint8, v uint32) { c.Regs[reg&7] = v }

func (c *CPU) fetch8() (byte, error) {
	v, err := c.Mem.Fetch8(c.EIP)
	c.EIP++
	return v, err
}
func (c *CPU) fetch16() (uint16, error) {
	lo, err := c.fetch8()
	if err != nil {
		return 0, err
	}
	hi, err := c.fetch8()
	if err != nil {
		return 0, err
	}
	return uint16(lo) | uint16(hi)<<8, nil
}

func (c *CPU) fetch32() (uint32, error) {
	b0, err := c.fetch8()
	if err != nil {
		return 0, err
	}
	b1, err := c.fetch8()
	if err != nil {
		return 0, err
	}
	b2, err := c.fetch8()
	if err != nil {
		return 0, err
	}
	b3, err := c.fetch8()
	if err != nil {
		return 0, err
	}
	return uint32(b0) | uint32(b1)<<8 | uint32(b2)<<16 | uint32(b3)<<24, nil
}
func (c *CPU) push(v uint32) error {
	if c.Regs[ESP] < 4 {
		return fmt.Errorf("stack underflow")
	}
	c.Regs[ESP] -= 4
	return c.Mem.Write32(c.Regs[ESP], v)
}
func (c *CPU) pop() (uint32, error) {
	v, err := c.Mem.Read32(c.Regs[ESP])
	if err != nil {
		return 0, err
	}
	c.Regs[ESP] += 4
	return v, nil
}
func (c *CPU) setAddFlags(a, b, result uint32) {
	c.EFLAGS &^= FlagCF | FlagPF | FlagZF | FlagSF | FlagOF
	if result < a {
		c.EFLAGS |= FlagCF
	}
	if result == 0 {
		c.EFLAGS |= FlagZF
	}
	if result&0x80000000 != 0 {
		c.EFLAGS |= FlagSF
	}
	if ((^(a ^ b)) & (a ^ result) & 0x80000000) != 0 {
		c.EFLAGS |= FlagOF
	}
	if evenParity(result) {
		c.EFLAGS |= FlagPF
	}
}
func (c *CPU) setLogicFlagsN(result uint32, bits uint) {
	mask := uint32((uint64(1) << bits) - 1)
	result &= mask
	c.EFLAGS &^= FlagCF | FlagPF | FlagOF | FlagZF | FlagSF
	if result == 0 {
		c.EFLAGS |= FlagZF
	}
	if result&(uint32(1)<<(bits-1)) != 0 {
		c.EFLAGS |= FlagSF
	}
	if evenParity(result) {
		c.EFLAGS |= FlagPF
	}
}

func (c *CPU) setAddFlagsN(a, b, result uint32, bits uint) {
	mask := uint64((uint64(1) << bits) - 1)
	sum := uint64(a&uint32(mask)) + uint64(b&uint32(mask))
	res := uint32(sum & mask)
	sign := uint32(1) << (bits - 1)
	c.EFLAGS &^= FlagCF | FlagPF | FlagOF | FlagZF | FlagSF
	if sum > mask {
		c.EFLAGS |= FlagCF
	}
	if res == 0 {
		c.EFLAGS |= FlagZF
	}
	if res&sign != 0 {
		c.EFLAGS |= FlagSF
	}
	if ((^(a ^ b)) & (a ^ res) & sign) != 0 {
		c.EFLAGS |= FlagOF
	}
	if evenParity(res) {
		c.EFLAGS |= FlagPF
	}
}

func (c *CPU) setSubFlagsN(a, b, result uint32, bits uint) {
	mask := uint32((uint64(1) << bits) - 1)
	res := result & mask
	sign := uint32(1) << (bits - 1)
	c.EFLAGS &^= FlagCF | FlagPF | FlagOF | FlagZF | FlagSF
	if (a & mask) < (b & mask) {
		c.EFLAGS |= FlagCF
	}
	if res == 0 {
		c.EFLAGS |= FlagZF
	}
	if res&sign != 0 {
		c.EFLAGS |= FlagSF
	}
	if ((a ^ b) & (a ^ res) & sign) != 0 {
		c.EFLAGS |= FlagOF
	}
	if evenParity(res) {
		c.EFLAGS |= FlagPF
	}
}

func (c *CPU) setLogicFlags(result uint32) {
	c.EFLAGS &^= FlagCF | FlagPF | FlagOF | FlagZF | FlagSF
	if result == 0 {
		c.EFLAGS |= FlagZF
	}
	if result&0x80000000 != 0 {
		c.EFLAGS |= FlagSF
	}
	if evenParity(result) {
		c.EFLAGS |= FlagPF
	}
}

func evenParity(value uint32) bool {
	b := byte(value)
	count := 0
	for b != 0 {
		count++
		b &= b - 1
	}
	return count%2 == 0
}

func (c *CPU) setADCFlagsN(a, b, carry, result uint32, bits uint) {
	mask := uint64((uint64(1) << bits) - 1)
	sum := uint64(a&uint32(mask)) + uint64(b&uint32(mask)) + uint64(carry)
	res := uint32(sum & mask)
	sign := uint32(1) << (bits - 1)
	c.EFLAGS &^= FlagCF | FlagPF | FlagOF | FlagZF | FlagSF
	if sum > mask {
		c.EFLAGS |= FlagCF
	}
	if res == 0 {
		c.EFLAGS |= FlagZF
	}
	if res&sign != 0 {
		c.EFLAGS |= FlagSF
	}
	if ((^(a ^ b))&(a^res)&sign) != 0 || (carry != 0 && ((a^b)&sign) == 0 && ((a^res)&sign) != 0) {
		c.EFLAGS |= FlagOF
	}
	if evenParity(res) {
		c.EFLAGS |= FlagPF
	}
}

func (c *CPU) setSBBFlagsN(a, b, borrow, result uint32, bits uint) {
	mask := uint32((uint64(1) << bits) - 1)
	subtrahend := b + borrow
	res := result & mask
	sign := uint32(1) << (bits - 1)
	c.EFLAGS &^= FlagCF | FlagPF | FlagOF | FlagZF | FlagSF
	if (a & mask) < subtrahend {
		c.EFLAGS |= FlagCF
	}
	if res == 0 {
		c.EFLAGS |= FlagZF
	}
	if res&sign != 0 {
		c.EFLAGS |= FlagSF
	}
	if ((a ^ subtrahend) & (a ^ res) & sign) != 0 {
		c.EFLAGS |= FlagOF
	}
	if evenParity(res) {
		c.EFLAGS |= FlagPF
	}
}

func (c *CPU) setSubFlags(a, b, result uint32) {
	c.EFLAGS &^= FlagCF | FlagPF | FlagZF | FlagSF | FlagOF
	if a < b {
		c.EFLAGS |= FlagCF
	}
	if result == 0 {
		c.EFLAGS |= FlagZF
	}
	if result&0x80000000 != 0 {
		c.EFLAGS |= FlagSF
	}
	if ((a ^ b) & (a ^ result) & 0x80000000) != 0 {
		c.EFLAGS |= FlagOF
	}
	if evenParity(result) {
		c.EFLAGS |= FlagPF
	}
}
func (c *CPU) condition(op byte) bool {
	zf := c.EFLAGS&FlagZF != 0
	cf := c.EFLAGS&FlagCF != 0
	sf := c.EFLAGS&FlagSF != 0
	of := c.EFLAGS&FlagOF != 0
	switch op {
	case 0x0: // O
		return of
	case 0x1: // NO
		return !of
	case 0x4: // Z/E
		return zf
	case 0x5: // NZ/NE
		return !zf
	case 0x2: // B/NAE/C
		return cf
	case 0x3: // AE/NC
		return !cf
	case 0x6: // BE/NA
		return cf || zf
	case 0x7: // A/NBE
		return !cf && !zf
	case 0x8: // S
		return sf
	case 0x9: // NS
		return !sf
	case 0xA: // P/PE
		return c.EFLAGS&FlagPF != 0
	case 0xB: // NP/PO
		return c.EFLAGS&FlagPF == 0
	case 0xC: // L
		return sf != of
	case 0xD: // GE
		return sf == of
	case 0xF: // G
		return !zf && sf == of
	case 0xE: // LE
		return zf || sf != of
	default:
		return false
	}
}

func (c *CPU) Step() error {
	if c.Halted {
		return nil
	}
	start := c.EIP
	c.segmentBase = 0
	operand16 := false
	address16 := false
	rep := false
	var op byte
	var err error
prefixLoop:
	for {
		op, err = c.fetch8()
		if err != nil {
			return err
		}
		switch op {
		case 0x66:
			operand16 = true
		case 0x67:
			address16 = true
		case 0x26, 0x2E, 0x36, 0x3E, 0xF0, 0xF2:
		case 0xF3:
			rep = true
		case 0x64:
			c.segmentBase = c.FSBase
		case 0x65:
			c.segmentBase = c.GSBase
		default:
			break prefixLoop
		}
	}
	_ = operand16
	_ = address16
	c.LastEIP = start
	c.LastOpcode = op
	switch {
	case op >= 0xB0 && op <= 0xB7: // MOV r8, imm8
		v, err := c.fetch8()
		if err != nil {
			return err
		}
		c.setByteReg(op-0xB0, v)
	case op >= 0xB8 && op <= 0xBF: // MOV r32, imm32
		v, err := c.fetch32()
		if err != nil {
			return err
		}
		c.Regs[op-0xB8] = v
	case op >= 0x50 && op <= 0x57: // PUSH r32
		if err := c.push(c.Regs[op-0x50]); err != nil {
			return err
		}
	case op >= 0x58 && op <= 0x5F: // POP r32
		v, err := c.pop()
		if err != nil {
			return err
		}
		c.Regs[op-0x58] = v
	case op >= 0x40 && op <= 0x47: // INC r32
		a := c.Regs[op-0x40]
		c.Regs[op-0x40]++
		c.setAddFlags(a, 1, c.Regs[op-0x40])
		c.EFLAGS &^= FlagCF // INC does not modify carry.
	case op >= 0x48 && op <= 0x4F: // DEC r32
		a := c.Regs[op-0x48]
		c.Regs[op-0x48]--
		c.setSubFlags(a, 1, c.Regs[op-0x48])
		c.EFLAGS &^= FlagCF // DEC does not modify carry.
	case op == 0x68: // PUSH imm32
		v, err := c.fetch32()
		if err != nil {
			return err
		}
		if err := c.push(v); err != nil {
			return err
		}
	case op == 0x6A: // PUSH imm8 sign extended
		v, err := c.fetch8()
		if err != nil {
			return err
		}
		if err := c.push(uint32(int32(int8(v)))); err != nil {
			return err
		}
	case op == 0x90: // NOP
	case op == 0x99: // CDQ: sign-extend EAX into EDX:EAX
		if c.Regs[EAX]&0x80000000 != 0 {
			c.Regs[EDX] = 0xffffffff
		} else {
			c.Regs[EDX] = 0
		}
	case op >= 0x91 && op <= 0x97: // XCHG EAX, r32
		reg := op - 0x90
		c.Regs[EAX], c.Regs[reg] = c.Regs[reg], c.Regs[EAX]
	case op == 0xF4: // HLT
		c.Halted = true
	case op == 0xD8 || op == 0xD9 || op == 0xDB || op == 0xDC || op == 0xDD || op == 0xDE || op == 0xDF: // small x87 subset
		if err := c.stepX87(op, start); err != nil {
			return err
		}
	case op == 0x9E: // SAHF
		ah := byte(c.Regs[EAX] >> 8)
		c.EFLAGS &^= FlagSF | FlagZF | FlagPF | FlagCF
		if ah&0x80 != 0 {
			c.EFLAGS |= FlagSF
		}
		if ah&0x40 != 0 {
			c.EFLAGS |= FlagZF
		}
		if ah&0x04 != 0 {
			c.EFLAGS |= FlagPF
		}
		if ah&0x01 != 0 {
			c.EFLAGS |= FlagCF
		}

	case op == 0xF8: // CLC
		c.EFLAGS &^= FlagCF
	case op == 0xF9: // STC
		c.EFLAGS |= FlagCF
	case op == 0xFA: // CLI; interrupts are not delivered by this user-mode interpreter
		c.EFLAGS &^= FlagIF
	case op == 0xFB: // STI; interrupts are not delivered by this user-mode interpreter
		c.EFLAGS |= FlagIF
	case op == 0xFC: // CLD
		c.EFLAGS &^= FlagDF
	case op == 0xFD: // STD
		c.EFLAGS |= FlagDF
	case op == 0xC3: // RET
		v, err := c.pop()
		if err != nil {
			return err
		}
		c.EIP = v
	case op == 0xC2: // RET imm16
		v, err := c.pop()
		if err != nil {
			return err
		}
		adjust, err := c.fetch16()
		if err != nil {
			return err
		}
		c.Regs[ESP] += uint32(adjust)
		c.EIP = v
	case op == 0xC9: // LEAVE
		c.Regs[ESP] = c.Regs[EBP]
		v, err := c.pop()
		if err != nil {
			return err
		}
		c.Regs[EBP] = v
	case op == 0xE8: // CALL rel32
		rel, err := c.fetch32()
		if err != nil {
			return err
		}
		if err := c.push(c.EIP); err != nil {
			return err
		}
		c.EIP += rel
	case op == 0xE9: // JMP rel32
		rel, err := c.fetch32()
		if err != nil {
			return err
		}
		c.EIP += rel
	case op == 0xEB: // JMP rel8
		rel, err := c.fetch8()
		if err != nil {
			return err
		}
		c.EIP += uint32(int32(int8(rel)))
	case op >= 0x70 && op <= 0x7F: // Jcc rel8
		rel, err := c.fetch8()
		if err != nil {
			return err
		}
		if c.condition(op & 0xF) {
			c.EIP += uint32(int32(int8(rel)))
		}
	case op == 0x0F: // extended 0F opcode map
		ext, err := c.fetch8()
		if err != nil {
			return err
		}
		switch {
		case ext >= 0x80 && ext <= 0x8F: // Jcc rel32
			rel, err := c.fetch32()
			if err != nil {
				return err
			}
			if c.condition(ext & 0x0F) {
				c.EIP += rel
			}
		case ext >= 0xC8 && ext <= 0xCF: // BSWAP r32
			reg := ext & 7
			c.Regs[reg] = bits.ReverseBytes32(c.Regs[reg])
		case ext >= 0x90 && ext <= 0x9F: // SETcc r/m8
			m, err := c.decodeModRM()
			if err != nil {
				return err
			}
			value := byte(0)
			if c.condition(ext & 0x0F) {
				value = 1
			}
			if err := c.writeRM8(m, value); err != nil {
				return err
			}
		case ext == 0x1F: // multi-byte NOP
			m, err := c.decodeModRM()
			if err != nil {
				return err
			}
			_ = m
		case ext == 0xA4: // SHLD r/m32, r32, imm8
			m, err := c.decodeModRM()
			if err != nil {
				return err
			}
			countByte, err := c.fetch8()
			if err != nil {
				return err
			}
			count := uint32(countByte & 31)
			if count == 0 {
				break
			}
			dest, err := c.readRM(m)
			if err != nil {
				return err
			}
			src := c.Regs[m.reg]
			result := (dest << count) | (src >> (32 - count))
			cf := dest&(uint32(1)<<(32-count)) != 0
			of := count == 1 && ((dest>>31)&1 != (result>>31)&1)
			if err := c.writeRM(m, result); err != nil {
				return err
			}
			c.setShiftFlags(result, cf, of)
		case ext == 0xAC: // SHRD r/m32, r32, imm8
			m, err := c.decodeModRM()
			if err != nil {
				return err
			}
			countByte, err := c.fetch8()
			if err != nil {
				return err
			}
			count := uint32(countByte & 31)
			if count == 0 {
				break
			}
			dest, err := c.readRM(m)
			if err != nil {
				return err
			}
			src := c.Regs[m.reg]
			result := (dest >> count) | (src << (32 - count))
			cf := dest&(uint32(1)<<(count-1)) != 0
			of := count == 1 && ((dest>>31)&1 != (result>>31)&1)
			if err := c.writeRM(m, result); err != nil {
				return err
			}
			c.setShiftFlags(result, cf, of)
		case ext == 0xBA: // BT/BTS/BTR/BTC r/m32, imm8
			m, err := c.decodeModRM()
			if err != nil {
				return err
			}
			imm, err := c.fetch8()
			if err != nil {
				return err
			}
			if m.reg < 4 || m.reg > 7 {
				return fmt.Errorf("unsupported BA group /%d at 0x%08x", m.reg, start)
			}
			value, err := c.readRM(m)
			if err != nil {
				return err
			}
			bit := uint32(imm & 31)
			if value&(1<<bit) != 0 {
				c.EFLAGS |= FlagCF
			} else {
				c.EFLAGS &^= FlagCF
			}
			switch m.reg {
			case 5:
				value |= 1 << bit
			case 6:
				value &^= 1 << bit
			case 7:
				value ^= 1 << bit
			}
			if m.reg != 4 {
				if err := c.writeRM(m, value); err != nil {
					return err
				}
			}
		case ext == 0xA3 || ext == 0xAB || ext == 0xB3 || ext == 0xBB: // BT/BTS/BTR/BTC
			m, err := c.decodeModRM()
			if err != nil {
				return err
			}
			bitIndex := int32(c.Regs[m.reg])
			bit := uint32(bitIndex) & 31
			var value uint32
			var bitAddr uint32
			if m.isReg {
				value, err = c.readRM(m)
			} else {
				wordOffset := int64(bitIndex) / 32
				bitAddr = m.addr + uint32(wordOffset*4) + c.segmentBase
				value, err = c.Mem.Read32(bitAddr)
			}
			if err != nil {
				return err
			}
			if value&(1<<bit) != 0 {
				c.EFLAGS |= FlagCF
			} else {
				c.EFLAGS &^= FlagCF
			}
			switch ext {
			case 0xAB:
				value |= 1 << bit
			case 0xB3:
				value &^= 1 << bit
			case 0xBB:
				value ^= 1 << bit
			}
			if ext != 0xA3 {
				if m.isReg {
					if err := c.writeRM(m, value); err != nil {
						return err
					}
				} else if err := c.Mem.Write32(bitAddr, value); err != nil {
					return err
				}
			}
		case ext == 0xBC || ext == 0xBD: // BSF/BSR r32, r/m32
			m, err := c.decodeModRM()
			if err != nil {
				return err
			}
			value, err := c.readRM(m)
			if err != nil {
				return err
			}
			if value == 0 {
				c.EFLAGS |= FlagZF
				break
			}
			var index uint32
			if ext == 0xBC {
				for (value & 1) == 0 {
					value >>= 1
					index++
				}
			} else {
				index = 31
				for index > 0 && value&(uint32(1)<<index) == 0 {
					index--
				}
			}
			c.Regs[m.reg] = index
			c.EFLAGS &^= FlagZF
		case ext == 0xB1: // CMPXCHG r/m32, r32

			m, err := c.decodeModRM()
			if err != nil {
				return err
			}
			dest, err := c.readRM(m)
			if err != nil {
				return err
			}
			compare := c.Regs[EAX]
			c.setSubFlags(compare, dest, compare-dest)
			if compare == dest {
				c.EFLAGS |= FlagZF
				if err := c.writeRM(m, c.Regs[m.reg]); err != nil {
					return err
				}
			} else {
				c.EFLAGS &^= FlagZF
				c.Regs[EAX] = dest
			}
		case ext == 0xAF: // IMUL r32, r/m32

			m, err := c.decodeModRM()
			if err != nil {
				return err
			}
			value, err := c.readRM(m)
			if err != nil {
				return err
			}
			result := uint32(int32(c.Regs[m.reg]) * int32(value))
			c.Regs[m.reg] = result
			c.setLogicFlags(result)
		case ext == 0xB6 || ext == 0xB7 || ext == 0xBE || ext == 0xBF: // MOVZX/MOVSX
			m, err := c.decodeModRM()
			if err != nil {
				return err
			}
			var value uint32
			if ext == 0xB6 || ext == 0xBE {
				v, readErr := c.readRM8(m)
				if readErr != nil {
					return readErr
				}
				if ext == 0xBE {
					value = uint32(int32(int8(v)))
				} else {
					value = uint32(v)
				}
			} else {
				v, readErr := c.readRM16(m)
				if readErr != nil {
					return readErr
				}
				if ext == 0xBF {
					value = uint32(int32(int16(v)))
				} else {
					value = uint32(v)
				}
			}
			c.Regs[m.reg] = value
		default:
			return fmt.Errorf("unsupported 0F opcode 0x%02x at 0x%08x", ext, start)
		}
	case op == 0xFE: // INC/DEC r/m8
		m, err := c.decodeModRM()
		if err != nil {
			return err
		}
		if m.reg != 0 && m.reg != 1 {
			return fmt.Errorf("unsupported FE group /%d at 0x%08x", m.reg, start)
		}
		value, err := c.readRM8(m)
		if err != nil {
			return err
		}
		oldCF := c.EFLAGS & FlagCF
		var result byte
		if m.reg == 0 {
			result = value + 1
			c.setAddFlagsN(uint32(value), 1, uint32(result), 8)
		} else {
			result = value - 1
			c.setSubFlagsN(uint32(value), 1, uint32(result), 8)
		}
		c.EFLAGS = (c.EFLAGS &^ FlagCF) | oldCF
		if err := c.writeRM8(m, result); err != nil {
			return err
		}
	case op == 0xFF: // INC/DEC/CALL/JMP/PUSH r/m32
		m, err := c.decodeModRM()
		if err != nil {
			return err
		}
		value, err := c.readRM(m)
		if err != nil {
			return err
		}
		switch m.reg {
		case 0:
			oldCF := c.EFLAGS & FlagCF
			result := value + 1
			c.setAddFlags(value, 1, result)
			c.EFLAGS = (c.EFLAGS &^ FlagCF) | oldCF
			if err := c.writeRM(m, result); err != nil {
				return err
			}
		case 1:
			oldCF := c.EFLAGS & FlagCF
			result := value - 1
			c.setSubFlags(value, 1, result)
			c.EFLAGS = (c.EFLAGS &^ FlagCF) | oldCF
			if err := c.writeRM(m, result); err != nil {
				return err
			}
		case 2: // CALL r/m32
			if err := c.push(c.EIP); err != nil {
				return err
			}
			c.EIP = value
		case 4: // JMP r/m32
			c.EIP = value
		case 6: // PUSH r/m32
			if err := c.push(value); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported FF group /%d at 0x%08x", m.reg, start)
		}
	case op == 0xF6 || op == 0xF7: // TEST/NOT/NEG/MUL/DIV groups
		m, err := c.decodeModRM()
		if err != nil {
			return err
		}
		if operand16 && op == 0xF7 {
			if m.reg != 0 {
				return fmt.Errorf("unsupported 16-bit F7 group /%d at 0x%08x", m.reg, start)
			}
			value, readErr := c.readRM16(m)
			if readErr != nil {
				return readErr
			}
			imm, fetchErr := c.fetch16()
			if fetchErr != nil {
				return fetchErr
			}
			c.setLogicFlagsN(uint32(value&imm), 16)
			break
		}
		bits := uint(32)
		if op == 0xF6 {
			bits = 8
		}
		var value uint32
		if bits == 8 {
			v, readErr := c.readRM8(m)
			if readErr != nil {
				return readErr
			}
			value = uint32(v)
		} else {
			value, err = c.readRM(m)
			if err != nil {
				return err
			}
		}
		switch m.reg {
		case 0: // TEST
			var imm uint32
			if bits == 8 {
				v, fetchErr := c.fetch8()
				if fetchErr != nil {
					return fetchErr
				}
				imm = uint32(v)
			} else {
				imm, err = c.fetch32()
				if err != nil {
					return err
				}
			}
			if bits == 8 {
				c.setLogicFlagsN(value&imm, 8)
			} else {
				c.setLogicFlags(value & imm)
			}
		case 2: // NOT
			if bits == 8 {
				if err := c.writeRM8(m, byte(^value)); err != nil {
					return err
				}
			} else if err := c.writeRM(m, ^value); err != nil {
				return err
			}
		case 3: // NEG
			result := -value
			if bits == 8 {
				c.setSubFlagsN(0, value, result, 8)
				if err := c.writeRM8(m, byte(result)); err != nil {
					return err
				}
			} else {
				c.setSubFlags(0, value, result)
				if err := c.writeRM(m, result); err != nil {
					return err
				}
			}
		case 4, 5: // MUL/IMUL
			if bits == 8 {
				if m.reg == 4 {
					product := uint16(c.byteReg(0)) * uint16(byte(value))
					c.Regs[EAX] = (c.Regs[EAX] & 0xffff0000) | uint32(product)
				} else {
					product := int16(int8(c.byteReg(0))) * int16(int8(byte(value)))
					c.Regs[EAX] = (c.Regs[EAX] & 0xffff0000) | uint32(uint16(product))
				}
			} else if m.reg == 4 {
				product := uint64(c.Regs[EAX]) * uint64(value)
				c.Regs[EAX] = uint32(product)
				c.Regs[EDX] = uint32(product >> 32)
			} else {
				product := int64(int32(c.Regs[EAX])) * int64(int32(value))
				c.Regs[EAX] = uint32(product)
				c.Regs[EDX] = uint32(uint64(product) >> 32)
			}
		case 6, 7: // DIV/IDIV
			if value == 0 {
				return fmt.Errorf("division by zero at 0x%08x", start)
			}
			if bits == 8 {
				dividend := uint32(c.Regs[EAX] & 0xffff)
				if m.reg == 6 {
					q, r := dividend/value, dividend%value
					if q > 0xff {
						return fmt.Errorf("byte division overflow")
					}
					c.setByteReg(0, byte(q))
					c.setByteReg(4, byte(r))
				} else {
					dividend := int32(int16(dividend))
					divisor := int32(int8(byte(value)))
					q, r := dividend/divisor, dividend%divisor
					if q < -128 || q > 127 {
						return fmt.Errorf("signed byte division overflow")
					}
					c.setByteReg(0, byte(int8(q)))
					c.setByteReg(4, byte(int8(r)))
				}
			} else {
				dividend := (uint64(c.Regs[EDX]) << 32) | uint64(c.Regs[EAX])
				if m.reg == 6 {
					q, r := dividend/uint64(value), dividend%uint64(value)
					if q > 0xffffffff {
						return fmt.Errorf("division overflow")
					}
					c.Regs[EAX] = uint32(q)
					c.Regs[EDX] = uint32(r)
				} else {
					signed := int64(dividend)
					divisor := int64(int32(value))
					q, r := signed/divisor, signed%divisor
					if q < -2147483648 || q > 2147483647 {
						return fmt.Errorf("signed division overflow")
					}
					c.Regs[EAX] = uint32(int32(q))
					c.Regs[EDX] = uint32(int32(r))
				}
			}
		default:
			return fmt.Errorf("unsupported F%d group /%d at 0x%08x", bits/8, m.reg, start)
		}
	case op == 0x69 || op == 0x6B: // IMUL r32, r/m32, immediate
		m, err := c.decodeModRM()
		if err != nil {
			return err
		}
		value, err := c.readRM(m)
		if err != nil {
			return err
		}
		var imm int32
		if op == 0x69 {
			v, fetchErr := c.fetch32()
			if fetchErr != nil {
				return fetchErr
			}
			imm = int32(v)
		} else {
			v, fetchErr := c.fetch8()
			if fetchErr != nil {
				return fetchErr
			}
			imm = int32(int8(v))
		}
		result := uint32(int64(int32(value)) * int64(imm))
		c.Regs[m.reg] = result
		c.setLogicFlags(result)
	case op == 0xC6: // MOV r/m8, imm8
		m, err := c.decodeModRM()
		if err != nil {
			return err
		}
		if m.reg != 0 {
			return fmt.Errorf("unsupported C6 group /%d at 0x%08x", m.reg, start)
		}
		value, err := c.fetch8()
		if err != nil {
			return err
		}
		if err := c.writeRM8(m, value); err != nil {
			return err
		}
	case op == 0xC0 || op == 0xD0: // shift/rotate r/m8, imm8 or by one
		m, err := c.decodeModRM()
		if err != nil {
			return err
		}
		count := byte(1)
		if op == 0xC0 {
			count, err = c.fetch8()
			if err != nil {
				return err
			}
		}
		count &= 7
		value, err := c.readRM8(m)
		if err != nil {
			return err
		}
		if count == 0 {
			break
		}
		result := value
		var cf, of bool
		switch m.reg {
		case 4:
			cf = (value>>(8-count))&1 != 0
			result = value << count
			of = count == 1 && (uint32((result>>7)&1) != boolBit(cf))
		case 5:
			cf = (value>>(count-1))&1 != 0
			result = value >> count
			of = count == 1 && value&0x80 != 0
		case 7:
			cf = (value>>(count-1))&1 != 0
			result = byte(int8(value) >> count)
		case 0:
			result = (value << count) | (value >> (8 - count))
			cf = result&1 != 0
			of = count == 1 && ((result>>7)&1 != (result & 1))
		case 1:
			result = (value >> count) | (value << (8 - count))
			cf = result&0x80 != 0
			of = count == 1 && ((result>>7)&1 != ((result >> 6) & 1))
		default:
			return fmt.Errorf("unsupported C0 group /%d at 0x%08x", m.reg, start)
		}
		if err := c.writeRM8(m, result); err != nil {
			return err
		}
		c.setShiftFlagsN(uint32(result), cf, of, 8)
	case op == 0xC1 || op == 0xD1 || op == 0xD3: // shift/rotate r/m16 or r/m32
		m, err := c.decodeModRM()
		if err != nil {
			return err
		}
		count := uint32(1)
		if op == 0xC1 {
			v, fetchErr := c.fetch8()
			if fetchErr != nil {
				return fetchErr
			}
			count = uint32(v & 31)
		} else if op == 0xD3 {
			count = uint32(c.byteReg(1) & 31)
		}
		bits := uint32(32)
		value, err := c.readRM(m)
		if err != nil {
			return err
		}
		if operand16 {
			bits = 16
			value &= 0xffff
		}
		if count == 0 {
			break
		}
		result := value
		var cf, of bool
		mask := uint32(0xffffffff)
		if bits == 16 {
			mask = 0xffff
		}
		switch m.reg {
		case 4: // SHL/SAL
			if count < bits {
				cf = (value>>(bits-count))&1 != 0
				result = (value << count) & mask
			} else {
				result = 0
			}
			of = count == 1 && ((result>>(bits-1))&1 != boolBit(cf))
		case 5: // SHR
			if count < bits {
				cf = (value>>(count-1))&1 != 0
				result = value >> count
			} else {
				result = 0
			}
			of = count == 1 && value&(uint32(1)<<(bits-1)) != 0
		case 7: // SAR
			if count < bits {
				cf = (value>>(count-1))&1 != 0
			} else {
				cf = value&(uint32(1)<<(bits-1)) != 0
			}
			if bits == 16 {
				result = uint32(uint16(int16(uint16(value)) >> count))
			} else {
				result = uint32(int32(value) >> count)
			}
			of = false
		case 0: // ROL
			count %= bits
			if count != 0 {
				result = ((value << count) | (value >> (bits - count))) & mask
			}
			cf = result&1 != 0
			of = count == 1 && ((result>>(bits-1))&1 != (result & 1))
		case 1: // ROR
			count %= bits
			if count != 0 {
				result = ((value >> count) | (value << (bits - count))) & mask
			}
			cf = result&(uint32(1)<<(bits-1)) != 0
			of = count == 1 && ((result>>(bits-1))&1 != ((result >> (bits - 2)) & 1))
		default:
			return fmt.Errorf("unsupported shift group /%d at 0x%08x", m.reg, start)
		}
		if operand16 {
			if err := c.writeRM16(m, uint16(result)); err != nil {
				return err
			}
			c.setShiftFlagsN(result, cf, of, 16)
		} else {
			if err := c.writeRM(m, result); err != nil {
				return err
			}
			c.setShiftFlags(result, cf, of)
		}

	case op == 0x38: // CMP r/m8, r8
		m, err := c.decodeModRM()
		if err != nil {
			return err
		}
		lhs, err := c.readRM8(m)
		if err != nil {
			return err
		}
		rhs := c.byteReg(m.reg)
		result := lhs - rhs
		c.setSubFlagsN(uint32(lhs), uint32(rhs), uint32(result), 8)
	case op == 0x80: // ALU r/m8, imm8
		m, err := c.decodeModRM()
		if err != nil {
			return err
		}
		imm, err := c.fetch8()
		if err != nil {
			return err
		}
		lhs, err := c.readRM8(m)
		if err != nil {
			return err
		}
		result := lhs
		write := true
		switch m.reg {
		case 0:
			result = lhs + imm
			c.setAddFlagsN(uint32(lhs), uint32(imm), uint32(result), 8)
		case 1:
			result = lhs | imm
			c.setLogicFlagsN(uint32(result), 8)
		case 4:
			result = lhs & imm
			c.setLogicFlagsN(uint32(result), 8)
		case 5:
			result = lhs - imm
			c.setSubFlagsN(uint32(lhs), uint32(imm), uint32(result), 8)
		case 6:
			result = lhs ^ imm
			c.setLogicFlagsN(uint32(result), 8)
		case 7:
			result = lhs - imm
			c.setSubFlagsN(uint32(lhs), uint32(imm), uint32(result), 8)
			write = false
		default:
			return fmt.Errorf("unsupported byte ALU group /%d at 0x%08x", m.reg, start)
		}
		if write {
			if err := c.writeRM8(m, byte(result)); err != nil {
				return err
			}
		}
	case op == 0x84: // TEST r/m8, r8
		m, err := c.decodeModRM()
		if err != nil {
			return err
		}
		lhs, err := c.readRM8(m)
		if err != nil {
			return err
		}
		c.setLogicFlagsN(uint32(lhs&c.byteReg(m.reg)), 8)
	case op == 0x87: // XCHG r32, r/m32
		m, err := c.decodeModRM()
		if err != nil {
			return err
		}
		value, err := c.readRM(m)
		if err != nil {
			return err
		}
		old := c.Regs[m.reg]
		c.Regs[m.reg] = value
		if err := c.writeRM(m, old); err != nil {
			return err
		}
	case op >= 0xA0 && op <= 0xA3: // MOV moffs
		addr, err := c.fetch32()
		if err != nil {
			return err
		}
		addr += c.segmentBase
		switch op {
		case 0xA0:
			value, readErr := c.Mem.Read8(addr)
			if readErr != nil {
				return readErr
			}
			c.setByteReg(0, value)
		case 0xA1:
			value, readErr := c.Mem.Read32(addr)
			if readErr != nil {
				return readErr
			}
			c.Regs[EAX] = value
		case 0xA2:
			if writeErr := c.Mem.Write8(addr, c.byteReg(0)); writeErr != nil {
				return writeErr
			}
		case 0xA3:
			if writeErr := c.Mem.Write32(addr, c.Regs[EAX]); writeErr != nil {
				return writeErr
			}
		}
	case op == 0xA4 || op == 0xA5: // MOVSB/MOVSD and REP variants
		count := uint32(1)
		if rep {
			count = c.Regs[ECX]
			c.Regs[ECX] = 0
		}
		width := uint32(1)
		if op == 0xA5 {
			width = 4
		}
		step := width
		if c.EFLAGS&FlagDF != 0 {
			step = ^width + 1
		}
		for i := uint32(0); i < count; i++ {
			if op == 0xA4 {
				value, readErr := c.Mem.Read8(c.Regs[ESI])
				if readErr != nil {
					return readErr
				}
				if writeErr := c.Mem.Write8(c.Regs[EDI], value); writeErr != nil {
					return writeErr
				}
			} else {
				value, readErr := c.Mem.Read32(c.Regs[ESI])
				if readErr != nil {
					return readErr
				}
				if writeErr := c.Mem.Write32(c.Regs[EDI], value); writeErr != nil {
					return writeErr
				}
			}
			c.Regs[ESI] += step
			c.Regs[EDI] += step
		}
	case op == 0xAA || op == 0xAB: // STOSB/STOSD and REP variants
		count := uint32(1)
		if rep {
			count = c.Regs[ECX]
			c.Regs[ECX] = 0
		}
		width := uint32(1)
		if op == 0xAB {
			width = 4
		}
		step := width
		if c.EFLAGS&FlagDF != 0 {
			step = ^step + 1
		}
		for i := uint32(0); i < count; i++ {
			if op == 0xAA {
				if err := c.Mem.Write8(c.Regs[EDI], c.byteReg(0)); err != nil {
					return err
				}
			} else if err := c.Mem.Write32(c.Regs[EDI], c.Regs[EAX]); err != nil {
				return err
			}
			c.Regs[EDI] += step
		}
	case op == 0x8E: // MOV Sreg, r/m16
		m, err := c.decodeModRM()
		if err != nil {
			return err
		}
		value, err := c.readRM16(m)
		if err != nil {
			return err
		}
		switch m.reg {
		case 1: // CS; loading CS is not valid for this user-mode model.
			return fmt.Errorf("unsupported MOV CS at 0x%08x", start)
		case 2: // SS
		case 3: // DS
		case 4: // FS
			c.FSSelector = value
		case 5: // GS
			c.GSSelector = value
		default:
			return fmt.Errorf("unsupported segment register %d at 0x%08x", m.reg, start)
		}
	case op == 0x88 || op == 0x8A: // MOV r/m8,r8 or MOV r8,r/m8
		m, err := c.decodeModRM()
		if err != nil {
			return err
		}
		if op == 0x88 {
			if err := c.writeRM8(m, c.byteReg(m.reg)); err != nil {
				return err
			}
		} else {
			value, err := c.readRM8(m)
			if err != nil {
				return err
			}
			c.setByteReg(m.reg, value)
		}
	case op == 0x0D || op == 0x25 || op == 0x35: // OR/AND/XOR EAX/AX, immediate
		if operand16 {
			imm, err := c.fetch16()
			if err != nil {
				return err
			}
			value := uint16(c.Regs[EAX])
			switch op {
			case 0x0D:
				value |= imm
			case 0x25:
				value &= imm
			case 0x35:
				value ^= imm
			}
			c.Regs[EAX] = (c.Regs[EAX] & 0xffff0000) | uint32(value)
			c.setLogicFlagsN(uint32(value), 16)
		} else {
			imm, err := c.fetch32()
			if err != nil {
				return err
			}
			switch op {
			case 0x0D:
				c.Regs[EAX] |= imm
			case 0x25:
				c.Regs[EAX] &= imm
			case 0x35:
				c.Regs[EAX] ^= imm
			}
			c.setLogicFlags(c.Regs[EAX])
		}

	case op == 0xA9: // TEST EAX, imm32
		imm, err := c.fetch32()
		if err != nil {
			return err
		}
		c.setLogicFlags(c.Regs[EAX] & imm)
	case op == 0xA8: // TEST AL, imm8
		imm, err := c.fetch8()
		if err != nil {
			return err
		}
		c.setLogicFlagsN(uint32(c.byteReg(0)&imm), 8)
	case op == 0x3C: // CMP AL, imm8
		imm, err := c.fetch8()
		if err != nil {
			return err
		}
		c.setSubFlagsN(uint32(c.byteReg(0)), uint32(imm), uint32(c.byteReg(0)-imm), 8)
	case op == 0x81 || op == 0x83: // ALU r/m32, imm32/imm8
		m, err := c.decodeModRM()
		if err != nil {
			return err
		}
		var rhs uint32
		bits := uint(32)
		if operand16 {
			bits = 16
			if op == 0x81 {
				v, fetchErr := c.fetch16()
				err = fetchErr
				rhs = uint32(v)
			} else {
				v, fetchErr := c.fetch8()
				err = fetchErr
				rhs = uint32(uint16(int16(int8(v))))
			}
		} else if op == 0x81 {
			rhs, err = c.fetch32()
		} else {
			v, fetchErr := c.fetch8()
			err = fetchErr
			rhs = uint32(int32(int8(v)))
		}
		if err != nil {
			return err
		}
		var lhs uint32
		if bits == 16 {
			v, readErr := c.readRM16(m)
			if readErr != nil {
				return readErr
			}
			lhs = uint32(v)
		} else {
			lhs, err = c.readRM(m)
			if err != nil {
				return err
			}
		}
		if err != nil {
			return err
		}
		result := lhs
		write := true
		switch m.reg {
		case 0: // ADD
			result = lhs + rhs
			if bits == 16 {
				c.setAddFlagsN(lhs, rhs, result, bits)
			} else {
				c.setAddFlags(lhs, rhs, result)
			}
		case 1: // OR
			result = lhs | rhs

			if bits == 16 {
				c.setLogicFlagsN(result, bits)
			} else {
				c.setLogicFlags(result)
			}
		case 2: // ADC
			carry := uint32(0)
			if c.EFLAGS&FlagCF != 0 {
				carry = 1
			}
			result = lhs + rhs + carry
			if bits == 16 {
				c.setADCFlagsN(lhs, rhs, carry, result, bits)
			} else {
				c.setADCFlagsN(lhs, rhs, carry, result, bits)
			}
		case 3: // SBB
			borrow := uint32(0)
			if c.EFLAGS&FlagCF != 0 {
				borrow = 1
			}
			result = lhs - rhs - borrow
			c.setSBBFlagsN(lhs, rhs, borrow, result, bits)
		case 4: // AND

			result = lhs & rhs
			if bits == 16 {
				c.setLogicFlagsN(result, bits)
			} else {
				c.setLogicFlags(result)
			}
		case 5: // SUB
			result = lhs - rhs
			if bits == 16 {
				c.setSubFlagsN(lhs, rhs, result, bits)
			} else {
				c.setSubFlags(lhs, rhs, result)
			}
		case 6: // XOR
			result = lhs ^ rhs
			if bits == 16 {
				c.setLogicFlagsN(result, bits)
			} else {
				c.setLogicFlags(result)
			}
		case 7: // CMP
			result = lhs - rhs
			if bits == 16 {
				c.setSubFlagsN(lhs, rhs, result, bits)
			} else {
				c.setSubFlags(lhs, rhs, result)
			}
			write = false
		default:
			return fmt.Errorf("unsupported ALU group /%d at 0x%08x", m.reg, start)
		}
		if write {
			if bits == 16 {
				if err := c.writeRM16(m, uint16(result)); err != nil {
					return err
				}
			} else if err := c.writeRM(m, result); err != nil {
				return err
			}
		}
	case op == 0x85: // TEST r/m32, r32
		m, err := c.decodeModRM()
		if err != nil {
			return err
		}
		lhs, err := c.readRM(m)
		if err != nil {
			return err
		}
		c.setLogicFlags(lhs & c.Regs[m.reg])
	case op == 0x00 || op == 0x02: // ADD r/m8,r8 or ADD r8,r/m8
		m, err := c.decodeModRM()
		if err != nil {
			return err
		}
		var lhs, rhs byte
		if op == 0x00 {
			lhs, err = c.readRM8(m)
			rhs = c.byteReg(m.reg)
		} else {
			lhs = c.byteReg(m.reg)
			rhs, err = c.readRM8(m)
		}
		if err != nil {
			return err
		}
		result := uint32(lhs) + uint32(rhs)
		if op == 0x00 {
			if err := c.writeRM8(m, byte(result)); err != nil {
				return err
			}
		} else {
			c.setByteReg(m.reg, byte(result))
		}
		c.setAddFlagsN(uint32(lhs), uint32(rhs), result, 8)
	case op == 0x04 || op == 0x05: // ADD AL/EAX, immediate
		if op == 0x04 {
			v, err := c.fetch8()
			if err != nil {
				return err
			}
			a := c.byteReg(0)
			result := uint32(a) + uint32(v)
			c.setByteReg(0, byte(result))
			c.setAddFlagsN(uint32(a), uint32(v), result, 8)
		} else {
			v, err := c.fetch32()
			if err != nil {
				return err
			}
			a := c.Regs[EAX]
			result := a + v
			c.Regs[EAX] = result
			c.setAddFlagsN(a, v, result, 32)
		}
	case op == 0x24 || op == 0x25: // AND AL/EAX, immediate

		if op == 0x24 {
			v, err := c.fetch8()
			if err != nil {
				return err
			}
			result := c.byteReg(0) & v
			c.setByteReg(0, result)
			c.setLogicFlagsN(uint32(result), 8)
		} else {
			v, err := c.fetch32()
			if err != nil {
				return err
			}
			result := c.Regs[EAX] & v
			c.Regs[EAX] = result
			c.setLogicFlagsN(result, 32)
		}
	case op == 0x0C || op == 0x0D: // OR AL/EAX, immediate

		if op == 0x0C {
			v, err := c.fetch8()
			if err != nil {
				return err
			}
			result := c.byteReg(0) | v
			c.setByteReg(0, result)
			c.setLogicFlagsN(uint32(result), 8)
		} else {
			v, err := c.fetch32()
			if err != nil {
				return err
			}
			result := c.Regs[EAX] | v
			c.Regs[EAX] = result
			c.setLogicFlagsN(result, 32)
		}
	case op == 0x14 || op == 0x15: // ADC AL/EAX, immediate

		carry := uint32(0)
		if c.EFLAGS&FlagCF != 0 {
			carry = 1
		}
		if op == 0x14 {
			v, err := c.fetch8()
			if err != nil {
				return err
			}
			a := c.byteReg(0)
			result := uint32(a) + uint32(v) + carry
			c.setByteReg(0, byte(result))
			c.setADCFlagsN(uint32(a), uint32(v), carry, result, 8)
		} else {
			v, err := c.fetch32()
			if err != nil {
				return err
			}
			a := c.Regs[EAX]
			result := a + v + carry
			c.Regs[EAX] = result
			c.setADCFlagsN(a, v, carry, result, 32)
		}
	case op == 0x05: // ADD EAX, imm32
		v, err := c.fetch32()
		if err != nil {
			return err
		}
		a := c.Regs[EAX]
		c.Regs[EAX] += v
		c.setAddFlags(a, v, c.Regs[EAX])
	case op == 0x2D: // SUB EAX, imm32
		v, err := c.fetch32()
		if err != nil {
			return err
		}
		a := c.Regs[EAX]
		c.Regs[EAX] -= v
		c.setSubFlags(a, v, c.Regs[EAX])
	case op == 0x3D: // CMP EAX, imm32
		v, err := c.fetch32()
		if err != nil {
			return err
		}
		c.setSubFlags(c.Regs[EAX], v, c.Regs[EAX]-v)
	case op == 0x1A: // SBB r8, r/m8
		m, err := c.decodeModRM()
		if err != nil {
			return err
		}
		lhs := uint32(c.byteReg(m.reg))
		rhsByte, err := c.readRM8(m)
		if err != nil {
			return err
		}
		borrow := c.EFLAGS & FlagCF
		if borrow != 0 {
			borrow = 1
		}
		result := lhs - uint32(rhsByte) - borrow
		c.setByteReg(m.reg, byte(result))
		c.setSBBFlagsN(lhs, uint32(rhsByte), borrow, result, 8)
	case op == 0x18 || op == 0x19 || op == 0x1B: // SBB r/m8/r/m32, r8/r32 or r32, r/m32
		m, err := c.decodeModRM()
		if err != nil {
			return err
		}
		borrow := uint32(0)
		if c.EFLAGS&FlagCF != 0 {
			borrow = 1
		}
		if op == 0x18 {
			lhs, readErr := c.readRM8(m)
			if readErr != nil {
				return readErr
			}
			rhs := uint32(c.byteReg(m.reg))
			result := uint32(lhs) - rhs - borrow
			if writeErr := c.writeRM8(m, byte(result)); writeErr != nil {
				return writeErr
			}
			c.setSBBFlagsN(uint32(lhs), rhs, borrow, result, 8)
		} else {
			bits := uint(32)
			var lhs, rhs uint32
			if operand16 {
				bits = 16
			}
			if op == 0x1B {
				if operand16 {
					lhs = uint32(uint16(c.Regs[m.reg]))
					rhs16, readErr := c.readRM16(m)
					if readErr != nil {
						return readErr
					}
					rhs = uint32(rhs16)
				} else {
					lhs = c.Regs[m.reg]
					rhs, err = c.readRM(m)
					if err != nil {
						return err
					}
				}
				result := lhs - rhs - borrow
				if operand16 {
					c.Regs[m.reg] = (c.Regs[m.reg] & 0xffff0000) | (result & 0xffff)
				} else {
					c.Regs[m.reg] = result
				}
				c.setSBBFlagsN(lhs, rhs, borrow, result, bits)
			} else {
				if operand16 {
					lhs16, readErr := c.readRM16(m)
					if readErr != nil {
						return readErr
					}
					lhs = uint32(lhs16)
					rhs = uint32(uint16(c.Regs[m.reg]))
				} else {
					lhs, err = c.readRM(m)
					if err != nil {
						return err
					}
					rhs = c.Regs[m.reg]
				}
				result := lhs - rhs - borrow
				if operand16 {
					if writeErr := c.writeRM16(m, uint16(result)); writeErr != nil {
						return writeErr
					}
				} else if writeErr := c.writeRM(m, result); writeErr != nil {
					return writeErr
				}
				c.setSBBFlagsN(lhs, rhs, borrow, result, bits)
			}
		}

	case op == 0x10 || op == 0x11 || op == 0x12 || op == 0x13: // ADC byte/dword register forms
		m, err := c.decodeModRM()
		if err != nil {
			return err
		}
		carry := uint32(0)
		if c.EFLAGS&FlagCF != 0 {
			carry = 1
		}
		if op == 0x10 || op == 0x12 {
			var lhs, rhs byte
			if op == 0x10 {
				lhs, err = c.readRM8(m)
				rhs = c.byteReg(m.reg)
			} else {
				lhs = c.byteReg(m.reg)
				rhs, err = c.readRM8(m)
			}
			if err != nil {
				return err
			}
			result := uint32(lhs) + uint32(rhs) + carry
			if op == 0x10 {
				if err := c.writeRM8(m, byte(result)); err != nil {
					return err
				}
			} else {
				c.setByteReg(m.reg, byte(result))
			}
			c.setADCFlagsN(uint32(lhs), uint32(rhs), carry, result, 8)
		} else {
			var lhs, rhs uint32
			if op == 0x11 {
				lhs, err = c.readRM(m)
				rhs = c.Regs[m.reg]
			} else {
				lhs = c.Regs[m.reg]
				rhs, err = c.readRM(m)
			}
			if err != nil {
				return err
			}
			result := lhs + rhs + carry
			if op == 0x11 {
				if err := c.writeRM(m, result); err != nil {
					return err
				}
			} else {
				c.Regs[m.reg] = result
			}
			c.setADCFlagsN(lhs, rhs, carry, result, 32)
		}
	case op == 0x08 || op == 0x0A: // OR r/m8,r8 or OR r8,r/m8
		m, err := c.decodeModRM()
		if err != nil {
			return err
		}
		var lhs, rhs byte
		if op == 0x08 {
			lhs, err = c.readRM8(m)
			rhs = c.byteReg(m.reg)
		} else {
			lhs = c.byteReg(m.reg)
			rhs, err = c.readRM8(m)
		}
		if err != nil {
			return err
		}
		result := lhs | rhs
		if op == 0x08 {
			if err := c.writeRM8(m, result); err != nil {
				return err
			}
		} else {
			c.setByteReg(m.reg, result)
		}
		c.setLogicFlagsN(uint32(result), 8)
	case op == 0x20 || op == 0x22: // AND r/m8,r8 or AND r8,r/m8
		m, err := c.decodeModRM()
		if err != nil {
			return err
		}
		var lhs, rhs byte
		if op == 0x20 {
			lhs, err = c.readRM8(m)
			rhs = c.byteReg(m.reg)
		} else {
			lhs = c.byteReg(m.reg)
			rhs, err = c.readRM8(m)
		}
		if err != nil {
			return err
		}
		result := lhs & rhs
		if op == 0x20 {
			if err := c.writeRM8(m, result); err != nil {
				return err
			}
		} else {
			c.setByteReg(m.reg, result)
		}
		c.setLogicFlagsN(uint32(result), 8)
	case op == 0x30: // XOR r/m8, r8
		m, err := c.decodeModRM()
		if err != nil {
			return err
		}
		lhs, err := c.readRM8(m)
		if err != nil {
			return err
		}
		result := lhs ^ c.byteReg(m.reg)
		if err := c.writeRM8(m, result); err != nil {
			return err
		}
		c.setLogicFlagsN(uint32(result), 8)
	case op == 0x09 || op == 0x0B || op == 0x21 || op == 0x23 || op == 0x2A || op == 0x3A:
		m, err := c.decodeModRM()
		if err != nil {
			return err
		}
		if op == 0x3A {
			lhs, readErr := c.readRM8(m)
			if readErr != nil {
				return readErr
			}
			rhs := c.byteReg(m.reg)
			result := rhs - lhs
			c.setSubFlagsN(uint32(rhs), uint32(lhs), uint32(result), 8)
			break
		}
		lhs, err := c.readRM(m)
		if err != nil {
			return err
		}
		rhs := c.Regs[m.reg]
		result := lhs
		destReg := op == 0x0B || op == 0x23 || op == 0x2A
		switch op {
		case 0x09, 0x0B:
			result = lhs | rhs
			c.setLogicFlags(result)
		case 0x21, 0x23:
			result = lhs & rhs
			c.setLogicFlags(result)
		case 0x2A:
			result = rhs - lhs
			c.setSubFlags(rhs, lhs, result)
		}
		if destReg {
			c.Regs[m.reg] = result
		} else if err := c.writeRM(m, result); err != nil {
			return err
		}
	case op == 0x01 || op == 0x03 || op == 0x29 || op == 0x2B || op == 0x31 || op == 0x33 || op == 0x39 || op == 0x3B || op == 0x89 || op == 0x8B || op == 0x8D || op == 0xC7:
		m, err := c.decodeModRM()
		if err != nil {
			return err
		}
		if op == 0xC7 {
			if m.reg != 0 {
				return fmt.Errorf("unsupported C7 group /%d at 0x%08x", m.reg, start)
			}
			if operand16 {
				value, err := c.fetch16()
				if err != nil {
					return err
				}
				if err := c.writeRM16(m, value); err != nil {
					return err
				}
			} else {
				value, err := c.fetch32()
				if err != nil {
					return err
				}
				if err := c.writeRM(m, value); err != nil {
					return err
				}
			}
			break
		}
		if op == 0x8D {
			if m.isReg {
				c.Regs[m.reg] = m.regVal
			} else {
				c.Regs[m.reg] = m.addr
			}
			break
		}
		if op == 0x89 || op == 0x8B {
			if operand16 {
				if op == 0x89 {
					if err := c.writeRM16(m, uint16(c.Regs[m.reg])); err != nil {
						return err
					}
				} else {
					value, err := c.readRM16(m)
					if err != nil {
						return err
					}
					c.Regs[m.reg] = (c.Regs[m.reg] & 0xffff0000) | uint32(value)
				}
				break
			}
			if op == 0x89 {
				if err := c.writeRM(m, c.Regs[m.reg]); err != nil {
					return err
				}
				break
			}
			value, err := c.readRM(m)
			if err != nil {
				return err
			}
			c.Regs[m.reg] = value
			break
		}
		rhs := c.Regs[m.reg]
		lhs, err := c.readRM(m)
		if err != nil {
			return err
		}
		var result uint32
		switch op {
		case 0x01, 0x03:
			result = lhs + rhs
			c.setAddFlags(lhs, rhs, result)
		case 0x29, 0x39:
			result = lhs - rhs
			c.setSubFlags(lhs, rhs, result)
		case 0x2B, 0x3B:
			result = rhs - lhs
			c.setSubFlags(rhs, lhs, result)
		case 0x31, 0x33:
			result = lhs ^ rhs
			c.setLogicFlags(result)
			if result == 0 {
				c.EFLAGS |= FlagZF
			}
			if result&0x80000000 != 0 {
				c.EFLAGS |= FlagSF
			}
		}
		if op == 0x01 || op == 0x29 || op == 0x31 {
			if err := c.writeRM(m, result); err != nil {
				return err
			}
		} else if op == 0x03 || op == 0x2B || op == 0x33 {
			c.Regs[m.reg] = result
		}
	case op == 0xCD: // INT imm8
		vector, err := c.fetch8()
		if err != nil {
			return err
		}
		if vector != 0x80 || c.OnSyscall == nil {
			return fmt.Errorf("unsupported interrupt 0x%02x at 0x%08x", vector, start)
		}
		if err := c.OnSyscall(c); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported opcode 0x%02x at 0x%08x", op, start)
	}
	c.Steps++
	return nil
}

func (c *CPU) fpuPush(value float64) error {
	if c.FPUCount >= 8 {
		return fmt.Errorf("x87 stack overflow")
	}
	c.FPUTop = (c.FPUTop + 7) & 7
	c.FPU[c.FPUTop] = value
	c.FPUCount++
	return nil
}

func (c *CPU) fpuPop() (float64, error) {
	if c.FPUCount == 0 {
		return 0, fmt.Errorf("x87 stack underflow")
	}
	value := c.FPU[c.FPUTop]
	c.FPUTop = (c.FPUTop + 1) & 7
	c.FPUCount--
	return value, nil
}

func (c *CPU) fpuST(index uint8) (float64, error) {
	if index >= c.FPUCount || index >= 8 {
		return 0, fmt.Errorf("invalid x87 stack register ST(%d)", index)
	}
	return c.FPU[(c.FPUTop+index)&7], nil
}

func (c *CPU) fpuCompare(lhs, rhs float64) {
	const statusCompareMask = uint16(0x4500) // C3, C2, C0
	c.FPUStatus &^= statusCompareMask
	if math.IsNaN(lhs) || math.IsNaN(rhs) {
		c.FPUStatus |= statusCompareMask
	} else if lhs < rhs {
		c.FPUStatus |= 0x0100 // C0
	} else if lhs == rhs {
		c.FPUStatus |= 0x4000 // C3
	}
}

func fpuFromExtended80(data []byte) float64 {
	if len(data) < 10 {
		return math.NaN()
	}
	significand := binary.LittleEndian.Uint64(data[:8])
	signExp := binary.LittleEndian.Uint16(data[8:10])
	exponent := signExp & 0x7fff
	if exponent == 0 {
		return 0
	}
	if exponent == 0x7fff {
		if significand == uint64(1)<<63 {
			if signExp&0x8000 != 0 {
				return math.Inf(-1)
			}
			return math.Inf(1)
		}
		return math.NaN()
	}
	value := math.Ldexp(float64(significand)/float64(uint64(1)<<63), int(exponent)-16383)
	if signExp&0x8000 != 0 {
		value = -value
	}
	return value
}

func fpuExtended80(value float64) [10]byte {
	sign := uint16(0)
	if math.Signbit(value) {
		sign = 0x8000
	}
	var significand uint64
	var extendedExp uint16
	if value == 0 {
		// Both signed zeros have an exponent and significand of zero.
	} else if math.IsInf(value, 0) || math.IsNaN(value) {
		extendedExp = 0x7fff
		if math.IsNaN(value) {
			significand = (uint64(1) << 63) | (uint64(1) << 62)
		} else {
			significand = uint64(1) << 63
		}
	} else {
		mantissa, exponent := math.Frexp(math.Abs(value))
		extendedExp = uint16(exponent - 1 + 16383)
		significand = uint64(mantissa * 2 * float64(uint64(1)<<63))
	}
	var data [10]byte
	binary.LittleEndian.PutUint64(data[:8], significand)
	binary.LittleEndian.PutUint16(data[8:], extendedExp|sign)
	return data
}

func fpuRound(value float64, control uint16) int64 {
	switch (control >> 10) & 3 {
	case 1: // round down
		return int64(math.Floor(value))
	case 2: // round up
		return int64(math.Ceil(value))
	case 3: // truncate toward zero
		return int64(math.Trunc(value))
	default: // round to nearest, ties to even (x87 default)
		return int64(math.RoundToEven(value))
	}
}

func (c *CPU) stepX87(op byte, start uint32) error {
	modrm, err := c.fetch8()
	if err != nil {
		return err
	}
	if op == 0xD9 && modrm == 0xEE { // FLDZ
		return c.fpuPush(0)
	}
	if op == 0xD9 && modrm == 0xE8 { // FLD1
		return c.fpuPush(1)
	}
	if op == 0xD9 && modrm == 0xE1 { // FABS
		value, err := c.fpuST(0)
		if err != nil {
			return err
		}
		c.FPU[c.FPUTop] = math.Abs(value)
		return nil
	}
	if op == 0xD9 && modrm == 0xFA { // FSQRT
		value, err := c.fpuST(0)
		if err != nil {
			return err
		}
		c.FPU[c.FPUTop] = math.Sqrt(value)
		return nil
	}
	if op == 0xD9 && modrm == 0xF8 { // FPREM
		dividend, err := c.fpuST(0)
		if err != nil {
			return err
		}
		divisor, err := c.fpuST(1)
		if err != nil {
			return err
		}
		c.FPU[c.FPUTop] = math.Mod(dividend, divisor)
		c.FPUStatus &^= 0x0400 // complete reduction: C2=0
		return nil
	}
	if op == 0xD9 && modrm == 0xE4 { // FTST
		value, err := c.fpuST(0)
		if err != nil {
			return err
		}
		c.fpuCompare(value, 0)
		return nil
	}
	if op == 0xDF && modrm == 0xE0 { // FNSTSW AX
		c.Regs[EAX] = (c.Regs[EAX] & 0xffff0000) | uint32(c.FPUStatus)
		return nil
	}
	if op == 0xD9 && modrm >= 0xC8 && modrm <= 0xCF { // FXCH ST(i)
		index := modrm & 7
		if index >= c.FPUCount {
			return fmt.Errorf("invalid x87 FXCH ST(%d) at 0x%08x", index, start)
		}
		st0 := c.FPU[c.FPUTop]
		slot := (c.FPUTop + index) & 7
		c.FPU[c.FPUTop], c.FPU[slot] = c.FPU[slot], st0
		return nil
	}
	if op == 0xD9 && modrm == 0xC0 { // FLD ST(0)
		value, err := c.fpuST(0)
		if err != nil {
			return err
		}
		return c.fpuPush(value)
	}
	if op == 0xD9 && ((modrm>>3)&7 == 5 || (modrm>>3)&7 == 7) && (modrm&0xc0) != 0xc0 { // FLDCW/FSTCW m16
		m, err := c.decodeModRMByte(modrm)
		if err != nil {
			return err
		}
		if m.isReg {
			return fmt.Errorf("unsupported x87 control register form at 0x%08x", start)
		}
		addr := m.addr + c.segmentBase
		if (modrm>>3)&7 == 5 {
			value, err := c.Mem.Read16(addr)
			if err != nil {
				return err
			}
			c.FPUControl = value
			return nil
		}
		return c.Mem.Write16(addr, c.FPUControl)
	}
	if op == 0xDB && (modrm>>3)&7 == 0 && (modrm&0xc0) != 0xc0 { // FILD m32int
		m, err := c.decodeModRMByte(modrm)
		if err != nil {
			return err
		}
		if m.isReg {
			return fmt.Errorf("unsupported x87 register FILD m32 at 0x%08x", start)
		}
		bits, err := c.Mem.Read32(m.addr + c.segmentBase)
		if err != nil {
			return err
		}
		return c.fpuPush(float64(int32(bits)))
	}
	if op == 0xDB && (modrm>>3)&7 == 5 && (modrm&0xc0) != 0xc0 { // FILD m80real
		m, err := c.decodeModRMByte(modrm)
		if err != nil {
			return err
		}
		if m.isReg {
			return fmt.Errorf("unsupported x87 register FILD m80 at 0x%08x", start)
		}
		data, err := c.Mem.ReadBytes(m.addr+c.segmentBase, 10)
		if err != nil {
			return err
		}
		return c.fpuPush(fpuFromExtended80(data))
	}
	if op == 0xDB && (modrm>>3)&7 == 7 && (modrm&0xc0) != 0xc0 { // FSTP m80real
		m, err := c.decodeModRMByte(modrm)
		if err != nil {
			return err
		}
		if m.isReg {
			return fmt.Errorf("unsupported x87 register FSTP m80 at 0x%08x", start)
		}
		value, err := c.fpuPop()
		if err != nil {
			return err
		}
		data := fpuExtended80(value)
		return c.Mem.WriteBytes(m.addr+c.segmentBase, data[:])
	}
	if op == 0xDB && (modrm>>3)&7 == 3 && (modrm&0xc0) != 0xc0 { // FISTP m32int
		m, err := c.decodeModRMByte(modrm)
		if err != nil {
			return err
		}
		if m.isReg {
			return fmt.Errorf("unsupported x87 register FISTP m32 at 0x%08x", start)
		}
		value, err := c.fpuPop()
		if err != nil {
			return err
		}
		integer := fpuRound(value, c.FPUControl)
		return c.Mem.Write32(m.addr+c.segmentBase, uint32(int32(integer)))
	}
	if op == 0xDF && (modrm>>3)&7 == 7 && (modrm&0xc0) != 0xc0 { // FISTP m64int
		m, err := c.decodeModRMByte(modrm)
		if err != nil {
			return err
		}
		if m.isReg {
			return fmt.Errorf("unsupported x87 register FISTP at 0x%08x", start)
		}
		value, err := c.fpuPop()
		if err != nil {
			return err
		}
		integer := fpuRound(value, c.FPUControl)
		var data [8]byte
		binary.LittleEndian.PutUint64(data[:], uint64(integer))
		return c.Mem.WriteBytes(m.addr+c.segmentBase, data[:])
	}
	if op == 0xDD && modrm >= 0xD8 && modrm <= 0xDF { // FSTP ST(i)
		if modrm == 0xD8 {
			_, err := c.fpuPop()
			return err
		}
		if modrm != 0xD9 {
			return fmt.Errorf("unsupported x87 register FSTP ST(%d) at 0x%08x", modrm&7, start)
		}
		value, err := c.fpuST(0)
		if err != nil {
			return err
		}
		c.FPU[(c.FPUTop+1)&7] = value
		_, err = c.fpuPop()
		return err
	}
	if op == 0xDE && modrm == 0xE2 { // FSUBRP ST(2), ST(0)
		lhs, err := c.fpuST(2)
		if err != nil {
			return err
		}
		rhs, err := c.fpuST(0)
		if err != nil {
			return err
		}
		c.FPU[(c.FPUTop+2)&7] = rhs - lhs
		_, err = c.fpuPop()
		return err
	}
	if op == 0xD8 && modrm == 0xC1 { // FADD ST(0), ST(1)
		lhs, err := c.fpuST(0)
		if err != nil {
			return err
		}
		rhs, err := c.fpuST(1)
		if err != nil {
			return err
		}
		c.FPU[c.FPUTop] = lhs + rhs
		return nil
	}
	if op == 0xDC && modrm == 0xC2 { // FADD ST(2), ST(0)
		lhs, err := c.fpuST(2)
		if err != nil {
			return err
		}
		rhs, err := c.fpuST(0)
		if err != nil {
			return err
		}
		c.FPU[(c.FPUTop+2)&7] = lhs + rhs
		return nil
	}
	if op == 0xDE && modrm == 0xC9 { // FMULP ST(1), ST(0)
		lhs, err := c.fpuST(1)
		if err != nil {
			return err
		}
		rhs, err := c.fpuST(0)
		if err != nil {
			return err
		}
		c.FPU[(c.FPUTop+1)&7] = lhs * rhs
		_, err = c.fpuPop()
		return err
	}
	if op == 0xDE && modrm == 0xE9 { // FSUBP ST(1), ST(0)
		lhs, err := c.fpuST(1)
		if err != nil {
			return err
		}
		rhs, err := c.fpuST(0)
		if err != nil {
			return err
		}
		c.FPU[(c.FPUTop+1)&7] = lhs - rhs
		_, err = c.fpuPop()
		return err
	}
	if op == 0xDE && modrm >= 0xC0 && modrm <= 0xC7 && (modrm>>3)&7 == 0 { // FADDP ST(i), ST(0)
		index := modrm & 7
		slot := (c.FPUTop + index) & 7
		lhs, err := c.fpuST(index)
		if err != nil {
			return err
		}
		rhs, err := c.fpuST(0)
		if err != nil {
			return err
		}
		c.FPU[slot] = lhs + rhs
		_, err = c.fpuPop()
		return err
	}
	if op == 0xD8 && modrm >= 0xD8 && modrm <= 0xDF { // FCOMP ST(i)
		if (modrm>>3)&7 != 3 {
			return fmt.Errorf("unsupported x87 D8 register form 0x%02x at 0x%08x", modrm, start)
		}
		lhs, err := c.fpuST(0)
		if err != nil {
			return err
		}
		rhs, err := c.fpuST(modrm & 7)
		if err != nil {
			return err
		}
		c.fpuCompare(lhs, rhs)
		_, err = c.fpuPop()
		return err
	}
	if op == 0xD8 && (modrm>>3)&7 == 0 { // FADD m32real
		m, err := c.decodeModRMByte(modrm)
		if err != nil {
			return err
		}
		if m.isReg {
			return fmt.Errorf("unsupported x87 register FADD m32 at 0x%08x", start)
		}
		bits, err := c.Mem.Read32(m.addr + c.segmentBase)
		if err != nil {
			return err
		}
		value, err := c.fpuST(0)
		if err != nil {
			return err
		}
		c.FPU[c.FPUTop] = value + float64(math.Float32frombits(bits))
		return nil
	}
	if op == 0xD8 && (modrm>>3)&7 == 1 { // FMUL m32real
		m, err := c.decodeModRMByte(modrm)
		if err != nil {
			return err
		}
		if m.isReg {
			return fmt.Errorf("unsupported x87 register FMUL m32 at 0x%08x", start)
		}
		bits, err := c.Mem.Read32(m.addr + c.segmentBase)
		if err != nil {
			return err
		}
		factor := float64(math.Float32frombits(bits))
		value, err := c.fpuST(0)
		if err != nil {
			return err
		}
		c.FPU[c.FPUTop] = value * factor
		return nil
	}
	if op == 0xDC && (modrm>>3)&7 == 1 { // FMUL m64real
		m, err := c.decodeModRMByte(modrm)
		if err != nil {
			return err
		}
		if m.isReg {
			return fmt.Errorf("unsupported x87 register FMUL at 0x%08x", start)
		}
		data, err := c.Mem.ReadBytes(m.addr+c.segmentBase, 8)
		if err != nil {
			return err
		}
		factor := math.Float64frombits(binary.LittleEndian.Uint64(data))
		value, err := c.fpuST(0)
		if err != nil {
			return err
		}
		c.FPU[c.FPUTop] = value * factor
		return nil
	}
	if op == 0xDF && (modrm>>3)&7 == 5 { // FILD m64int
		m, err := c.decodeModRMByte(modrm)
		if err != nil {
			return err
		}
		if m.isReg {
			return fmt.Errorf("unsupported x87 register FILD at 0x%08x", start)
		}
		data, err := c.Mem.ReadBytes(m.addr+c.segmentBase, 8)
		if err != nil {
			return err
		}
		return c.fpuPush(float64(int64(binary.LittleEndian.Uint64(data))))
	}
	if op == 0xD9 && (modrm>>3)&7 == 0 { // FLD m32real
		m, err := c.decodeModRMByte(modrm)
		if err != nil {
			return err
		}
		if m.isReg {
			return fmt.Errorf("unsupported x87 register FLD m32 at 0x%08x", start)
		}
		bits, err := c.Mem.Read32(m.addr + c.segmentBase)
		if err != nil {
			return err
		}
		return c.fpuPush(float64(math.Float32frombits(bits)))
	}
	if op == 0xDD && (modrm>>3)&7 == 0 { // FLD m64real
		m, err := c.decodeModRMByte(modrm)
		if err != nil {
			return err
		}
		if m.isReg {
			return fmt.Errorf("unsupported x87 register FLD at 0x%08x", start)
		}
		data, err := c.Mem.ReadBytes(m.addr+c.segmentBase, 8)
		if err != nil {
			return err
		}
		return c.fpuPush(math.Float64frombits(binary.LittleEndian.Uint64(data)))
	}
	if op == 0xDD && (modrm>>3)&7 == 2 { // FST m64real
		m, err := c.decodeModRMByte(modrm)
		if err != nil {
			return err
		}
		if m.isReg {
			return fmt.Errorf("unsupported x87 register FST at 0x%08x", start)
		}
		value, err := c.fpuST(0)
		if err != nil {
			return err
		}
		var data [8]byte
		binary.LittleEndian.PutUint64(data[:], math.Float64bits(value))
		return c.Mem.WriteBytes(m.addr+c.segmentBase, data[:])
	}
	if op == 0xDD && (modrm>>3)&7 == 3 { // FSTP m64real
		m, err := c.decodeModRMByte(modrm)
		if err != nil {
			return err
		}
		if m.isReg {
			return fmt.Errorf("unsupported x87 register FSTP at 0x%08x", start)
		}
		value, err := c.fpuPop()
		if err != nil {
			return err
		}
		var data [8]byte
		binary.LittleEndian.PutUint64(data[:], math.Float64bits(value))
		return c.Mem.WriteBytes(m.addr+c.segmentBase, data[:])
	}
	return fmt.Errorf("unsupported x87 opcode 0x%02x modrm 0x%02x at 0x%08x", op, modrm, start)
}

func (c *CPU) decodeModRMByte(v byte) (modRM, error) {
	// The x87 caller has already consumed the ModRM byte; decode its address
	// using the same displacement/SIB rules as ordinary integer operands.
	mod := v >> 6
	r := (v >> 3) & 7
	rm := v & 7
	out := modRM{reg: r, rm: rm}
	if mod == 3 {
		out.isReg = true
		out.regVal = c.Regs[rm]
		return out, nil
	}
	base := uint32(0)
	var err error
	if rm == 4 {
		sib, sibErr := c.fetch8()
		if sibErr != nil {
			return modRM{}, sibErr
		}
		scale := uint32(1) << (sib >> 6)
		index := (sib >> 3) & 7
		baseReg := sib & 7
		if index != 4 {
			base += c.Regs[index] * scale
		}
		if mod == 0 && baseReg == 5 {
			d, e := c.fetch32()
			err = e
			base += d
		} else {
			base += c.Regs[baseReg]
			if mod == 1 {
				d, e := c.fetch8()
				err = e
				base += uint32(int32(int8(d)))
			} else if mod == 2 {
				d, e := c.fetch32()
				err = e
				base += d
			}
		}
	} else if mod == 0 && rm == 5 {
		base, err = c.fetch32()
	} else {
		base = c.Regs[rm]
		if mod == 1 {
			d, e := c.fetch8()
			err = e
			base += uint32(int32(int8(d)))
		} else if mod == 2 {
			d, e := c.fetch32()
			err = e
			base += d
		}
	}
	if err != nil {
		return modRM{}, err
	}
	out.addr = base
	return out, nil
}

func (c *CPU) Run(maxSteps uint64) error {
	for !c.Halted && (maxSteps == 0 || c.Steps < maxSteps) {
		if err := c.Step(); err != nil {
			return err
		}
	}
	if !c.Halted && maxSteps != 0 {
		return fmt.Errorf("instruction limit exceeded: %d", maxSteps)
	}
	return nil
}

type modRM struct {
	reg    uint8
	rm     uint8
	isReg  bool
	regVal uint32
	addr   uint32
}

func (c *CPU) decodeModRM() (modRM, error) {
	v, err := c.fetch8()
	if err != nil {
		return modRM{}, err
	}
	mod := v >> 6
	r := (v >> 3) & 7
	rm := v & 7
	out := modRM{reg: r, rm: rm}
	if mod == 3 {
		out.isReg = true
		out.regVal = c.Regs[rm]
		return out, nil
	}
	base := uint32(0)
	if rm == 4 {
		sib, sibErr := c.fetch8()
		if sibErr != nil {
			return modRM{}, sibErr
		}
		scale := uint32(1) << (sib >> 6)
		index := (sib >> 3) & 7
		baseReg := sib & 7
		if index != 4 {
			base += c.Regs[index] * scale
		}
		if mod == 0 && baseReg == 5 {
			d, dErr := c.fetch32()
			err = dErr
			base += d
		} else {
			base += c.Regs[baseReg]
			if mod == 1 {
				d, dErr := c.fetch8()
				err = dErr
				base += uint32(int32(int8(d)))
			} else if mod == 2 {
				d, dErr := c.fetch32()
				err = dErr
				base += d
			}
		}
	} else if mod == 0 && rm == 5 {
		base, err = c.fetch32()
	} else {
		base = c.Regs[rm]
		if mod == 1 {
			d, e := c.fetch8()
			err = e
			base += uint32(int32(int8(d)))
		} else if mod == 2 {
			d, e := c.fetch32()
			err = e
			base += d
		}
	}
	if err != nil {
		return modRM{}, err
	}
	out.addr = base
	return out, nil
}

func boolBit(value bool) uint32 {
	if value {
		return 1
	}
	return 0
}

func (c *CPU) setShiftFlagsN(result uint32, cf, of bool, bits uint) {
	mask := uint32((uint64(1) << bits) - 1)
	result &= mask
	c.EFLAGS &^= FlagCF | FlagPF | FlagOF | FlagZF | FlagSF
	if cf {
		c.EFLAGS |= FlagCF
	}
	if of {
		c.EFLAGS |= FlagOF
	}
	if result == 0 {
		c.EFLAGS |= FlagZF
	}
	if result&(uint32(1)<<(bits-1)) != 0 {
		c.EFLAGS |= FlagSF
	}
	if evenParity(result) {
		c.EFLAGS |= FlagPF
	}
}

func (c *CPU) setShiftFlags(result uint32, cf, of bool) {
	c.EFLAGS &^= FlagCF | FlagPF | FlagOF | FlagZF | FlagSF
	if cf {
		c.EFLAGS |= FlagCF
	}
	if of {
		c.EFLAGS |= FlagOF
	}
	if result == 0 {
		c.EFLAGS |= FlagZF
	}
	if result&0x80000000 != 0 {
		c.EFLAGS |= FlagSF
	}
	if evenParity(result) {
		c.EFLAGS |= FlagPF
	}
}

func (c *CPU) byteReg(reg uint8) byte {
	reg &= 7
	if reg < 4 {
		return byte(c.Regs[reg])
	}
	return byte(c.Regs[reg-4] >> 8)
}

func (c *CPU) setByteReg(reg uint8, value byte) {
	reg &= 7
	if reg < 4 {
		c.Regs[reg] = (c.Regs[reg] &^ 0xff) | uint32(value)
		return
	}
	base := reg - 4
	c.Regs[base] = (c.Regs[base] &^ 0xff00) | uint32(value)<<8
}

func (c *CPU) writeRM16(m modRM, value uint16) error {
	if m.isReg {
		c.Regs[m.rm] = (c.Regs[m.rm] & 0xffff0000) | uint32(value)
		return nil
	}
	return c.Mem.Write16(m.addr+c.segmentBase, value)
}

func (c *CPU) readRM16(m modRM) (uint16, error) {
	if m.isReg {
		return uint16(c.Regs[m.rm]), nil
	}
	return c.Mem.Read16(m.addr + c.segmentBase)
}

func (c *CPU) readRM8(m modRM) (byte, error) {
	if m.isReg {
		return c.byteReg(m.rm), nil
	}
	return c.Mem.Read8(m.addr + c.segmentBase)
}

func (c *CPU) writeRM8(m modRM, value byte) error {
	if m.isReg {
		c.setByteReg(m.rm, value)
		return nil
	}
	return c.Mem.Write8(m.addr+c.segmentBase, value)
}

func (c *CPU) readRM(m modRM) (uint32, error) {
	if m.isReg {
		return m.regVal, nil
	}
	return c.Mem.Read32(m.addr + c.segmentBase)
}

func (c *CPU) writeRM(m modRM, value uint32) error {
	if m.isReg {
		c.Regs[m.rm] = value
		return nil
	}
	return c.Mem.Write32(m.addr+c.segmentBase, value)
}
