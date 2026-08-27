package elf32

import (
	"debug/elf"
	"encoding/binary"
	"fmt"

	"example.com/ish-go/internal/i386"
)

const (
	r386None     = 0
	r38632       = 1
	r386PC32     = 2
	r386GlobDat  = 6
	r386JumpSlot = 7
	r386Relative = 8
)

func applyEarlyRelativeRelocations(space *i386.AddressSpace, f *elf.File, data []byte, base uint32) error {
	tags, err := dynamicTags(f, data)
	if err != nil {
		return err
	}
	if relAddr, ok := tags[17]; ok {
		relSize := tags[18]
		if relSize%8 != 0 {
			return fmt.Errorf("invalid DT_REL size %d", relSize)
		}
		if relAddr+relSize > uint64(len(data)) {
			return fmt.Errorf("DT_REL outside ELF data")
		}
		for off := uint64(0); off < relSize; off += 8 {
			entry := data[relAddr+off : relAddr+off+8]
			target := binary.LittleEndian.Uint32(entry) + base
			info := binary.LittleEndian.Uint32(entry[4:])
			if uint8(info&0xff) != r386Relative {
				continue
			}
			value, readErr := space.Mem.Read32(target)
			if readErr != nil {
				return readErr
			}
			if writeErr := space.Mem.Write32(target, value+base); writeErr != nil {
				return writeErr
			}
		}
	}
	if relrAddr, ok := tags[36]; ok {
		relrSize := tags[35]
		if relrSize%4 != 0 {
			return fmt.Errorf("invalid DT_RELR size %d", relrSize)
		}
		if relrAddr+relrSize > uint64(len(data)) {
			return fmt.Errorf("DT_RELR outside ELF data")
		}
		if err := applyRELR(space, data[relrAddr:relrAddr+relrSize], base); err != nil {
			return fmt.Errorf("DT_RELR: %w", err)
		}
	}
	return nil
}

func dynamicTags(f *elf.File, data []byte) (map[uint64]uint64, error) {
	var dynamic *elf.Prog
	for _, prog := range f.Progs {
		if prog.Type == elf.PT_DYNAMIC {
			dynamic = prog
			break
		}
	}
	if dynamic == nil {
		return nil, fmt.Errorf("ELF has no PT_DYNAMIC")
	}
	if dynamic.Off+dynamic.Filesz > uint64(len(data)) || dynamic.Filesz%8 != 0 {
		return nil, fmt.Errorf("invalid PT_DYNAMIC")
	}
	tags := make(map[uint64]uint64)
	for off := dynamic.Off; off < dynamic.Off+dynamic.Filesz; off += 8 {
		tag := uint64(binary.LittleEndian.Uint32(data[off:]))
		value := uint64(binary.LittleEndian.Uint32(data[off+4:]))
		if tag == 0 {
			break
		}
		tags[tag] = value
	}
	return tags, nil
}

func applyDynamicRelocations(space *i386.AddressSpace, f *elf.File, base uint32, provider *elf.File, providerBase uint32) error {
	symbols, _ := f.DynamicSymbols()
	providerSymbols := symbols
	if provider != nil {
		providerSymbols, _ = provider.DynamicSymbols()
	}
	if sec := f.Section(".rel.dyn"); sec != nil {
		data, err := sec.Data()
		if err != nil {
			return err
		}
		if err := applyREL(space, data, symbols, providerSymbols, base, providerBase); err != nil {
			return fmt.Errorf(".rel.dyn: %w", err)
		}
	}
	if sec := f.Section(".rel.plt"); sec != nil {
		data, err := sec.Data()
		if err != nil {
			return err
		}
		if err := applyREL(space, data, symbols, providerSymbols, base, providerBase); err != nil {
			return fmt.Errorf(".rel.plt: %w", err)
		}
	}
	if sec := f.Section(".relr.dyn"); sec != nil {
		data, err := sec.Data()
		if err != nil {
			return err
		}
		if err := applyRELR(space, data, base); err != nil {
			return fmt.Errorf(".relr.dyn: %w", err)
		}
	}
	return nil
}

func applyREL(space *i386.AddressSpace, data []byte, symbols, provider []elf.Symbol, base, providerBase uint32) error {
	if len(data)%8 != 0 {
		return fmt.Errorf("invalid REL size %d", len(data))
	}
	for off := 0; off < len(data); off += 8 {
		target := binary.LittleEndian.Uint32(data[off:]) + base
		info := binary.LittleEndian.Uint32(data[off+4:])
		typ := uint8(info & 0xff)
		symIndex := info >> 8
		if typ == r386None {
			continue
		}
		addend, err := space.Mem.Read32(target)
		if err != nil {
			return err
		}
		var sym *elf.Symbol
		if symIndex != 0 && int(symIndex) < len(symbols) {
			sym = &symbols[symIndex]
		}
		symAddr, resolved := lookupSymbol(sym, symbols, base, provider, providerBase)
		switch typ {
		case r386Relative:
			if err := space.Mem.Write32(target, base+addend); err != nil {
				return err
			}
		case r38632:
			if !resolved && sym != nil && sym.Info>>4 != 2 { // non-weak unresolved
				return fmt.Errorf("unresolved symbol %q", sym.Name)
			}
			if err := space.Mem.Write32(target, symAddr+addend); err != nil {
				return err
			}
		case r386GlobDat, r386JumpSlot:
			if !resolved && sym != nil && sym.Info>>4 != 2 { // non-weak unresolved
				return fmt.Errorf("unresolved symbol %q", sym.Name)
			}
			if err := space.Mem.Write32(target, symAddr); err != nil {
				return err
			}
		case r386PC32:
			if err := space.Mem.Write32(target, symAddr+addend-target); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported relocation type %d", typ)
		}
	}
	return nil
}

func applyRELR(space *i386.AddressSpace, data []byte, base uint32) error {
	if len(data)%4 != 0 {
		return fmt.Errorf("invalid RELR size %d", len(data))
	}
	where := uint32(0)

	for off := 0; off < len(data); off += 4 {
		entry := binary.LittleEndian.Uint32(data[off:])
		if entry&1 == 0 {
			where = entry + base
			if err := applyRelativeWord(space, where, base); err != nil {
				return err
			}
			where += 4
			continue
		}
		bitmap := entry
		for bit := uint32(1); bit < 32; bit++ {
			if bitmap&(1<<bit) != 0 {
				if err := applyRelativeWord(space, where+(bit-1)*4, base); err != nil {
					return err
				}
			}
		}
		where += 31 * 4
	}
	return nil
}

func applyRelativeWord(space *i386.AddressSpace, addr, base uint32) error {
	value, err := space.Mem.Read32(addr)
	if err != nil {
		return err
	}
	return space.Mem.Write32(addr, value+base)
}

func lookupSymbol(sym *elf.Symbol, own []elf.Symbol, ownBase uint32, provider []elf.Symbol, providerBase uint32) (uint32, bool) {
	if sym == nil {
		return 0, true
	}
	if sym.Section != elf.SHN_UNDEF {
		return ownBase + uint32(sym.Value), true
	}
	for i := range provider {
		candidate := &provider[i]
		if candidate.Name == sym.Name && candidate.Section != elf.SHN_UNDEF {
			return providerBase + uint32(candidate.Value), true
		}
	}
	return 0, false
}
