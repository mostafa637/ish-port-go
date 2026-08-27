package i386

import (
	"fmt"
	"sort"
)

const (
	ProtRead uint8 = 1 << iota
	ProtWrite
	ProtExec
)

type Mapping struct {
	Start uint32
	End   uint32
	Prot  uint8
	Name  string
}

func (m Mapping) Size() uint32 { return m.End - m.Start }

// AddressSpace keeps Linux-like mapping metadata beside the bounded guest
// byte-addressed memory. The underlying memory remains non-executable host
// data; permissions are enforced at syscall/map boundaries.
type AddressSpace struct {
	Mem      *Memory
	Mappings []Mapping
	Max      uint32
	NextHint uint32
}

func NewAddressSpace(size uint32) *AddressSpace {
	if size < 64*1024 {
		size = 64 * 1024
	}
	return &AddressSpace{Mem: NewMemory(size), Max: size, NextHint: 0x01000000}
}

func AddressSpaceFromMemory(mem *Memory) *AddressSpace {
	return &AddressSpace{Mem: mem, Max: mem.Size(), NextHint: 0x01000000}
}

func (a *AddressSpace) Clone() *AddressSpace {
	copyMem := NewMemory(a.Mem.Size())
	data, _ := a.Mem.ReadRaw(0, a.Mem.Size())
	_ = copyMem.WriteRaw(0, data)
	mappings := append([]Mapping(nil), a.Mappings...)
	clone := &AddressSpace{Mem: copyMem, Mappings: mappings, Max: a.Max, NextHint: a.NextHint}
	if a.Mem.protected {
		clone.EnableProtection()
	}
	return clone
}

// EnableProtection switches guest accesses from bounds-only checks to mapping
// and permission checks. Call it after loader initialization is complete.
func (a *AddressSpace) EnableProtection() {
	if a != nil && a.Mem != nil {
		a.Mem.EnableProtection(a)
	}
}

func (a *AddressSpace) Add(start, size uint32, prot uint8, name string) error {
	if size == 0 || start > a.Max || size > a.Max-start {
		return fmt.Errorf("invalid mapping 0x%x+0x%x", start, size)
	}
	end := start + size
	for _, m := range a.Mappings {
		if start < m.End && end > m.Start {
			return fmt.Errorf("mapping overlaps 0x%x-0x%x", m.Start, m.End)
		}
	}
	a.Mappings = append(a.Mappings, Mapping{Start: start, End: end, Prot: prot, Name: name})
	sort.Slice(a.Mappings, func(i, j int) bool { return a.Mappings[i].Start < a.Mappings[j].Start })
	return nil
}

func (a *AddressSpace) Remove(start, size uint32) error {
	if size == 0 || start > a.Max || size > a.Max-start {
		return fmt.Errorf("invalid unmap 0x%x+0x%x", start, size)
	}
	end := start + size
	out := a.Mappings[:0]
	for _, m := range a.Mappings {
		if end <= m.Start || start >= m.End {
			out = append(out, m)
			continue
		}
		if start > m.Start {
			out = append(out, Mapping{Start: m.Start, End: start, Prot: m.Prot, Name: m.Name})
		}
		if end < m.End {
			out = append(out, Mapping{Start: end, End: m.End, Prot: m.Prot, Name: m.Name})
		}
	}
	a.Mappings = out
	return nil
}

func (a *AddressSpace) Protect(start, size uint32, prot uint8) error {
	end := start + size
	if size == 0 || end < start || end > a.Max {
		return fmt.Errorf("invalid protect range")
	}
	covered := false
	for i, m := range a.Mappings {
		if start >= m.Start && end <= m.End {
			covered = true
			parts := make([]Mapping, 0, len(a.Mappings)+2)
			parts = append(parts, a.Mappings[:i]...)
			if start > m.Start {
				parts = append(parts, Mapping{Start: m.Start, End: start, Prot: m.Prot, Name: m.Name})
			}
			parts = append(parts, Mapping{Start: start, End: end, Prot: prot, Name: m.Name})
			if end < m.End {
				parts = append(parts, Mapping{Start: end, End: m.End, Prot: m.Prot, Name: m.Name})
			}
			parts = append(parts, a.Mappings[i+1:]...)
			a.Mappings = parts
			break
		}
	}
	if !covered {
		return fmt.Errorf("protect range is not mapped")
	}
	return nil
}

func (a *AddressSpace) FindHole(size, hint uint32) (uint32, error) {
	if size == 0 || size > a.Max {
		return 0, fmt.Errorf("invalid hole size 0x%x", size)
	}
	if hint < 0x1000 {
		hint = 0x1000
	}
	candidates := []uint32{hint, a.NextHint}
	for _, candidate := range candidates {
		candidate &^= 0xfff
		if candidate > a.Max || size > a.Max-candidate {
			continue
		}
		if a.isFree(candidate, candidate+size) {
			a.NextHint = candidate + size
			return candidate, nil
		}
	}
	cursor := uint32(0x1000)
	for _, m := range a.Mappings {
		if cursor <= m.Start && size <= m.Start-cursor {
			a.NextHint = cursor + size
			return cursor, nil
		}
		if m.End > cursor {
			cursor = m.End
		}
	}
	if size <= a.Max-cursor {
		a.NextHint = cursor + size
		return cursor, nil
	}
	return 0, fmt.Errorf("no guest mapping hole for 0x%x bytes", size)
}

func (a *AddressSpace) checkRange(addr, width uint32, prot uint8) error {
	if width == 0 {
		return nil
	}
	if addr > a.Max || width > a.Max-addr {
		return fmt.Errorf("guest memory fault at 0x%08x (%d bytes)", addr, width)
	}
	end := addr + width
	cursor := addr
	for cursor < end {
		mapping, ok := a.MappingAt(cursor)
		if !ok {
			return fmt.Errorf("guest unmapped access at 0x%08x (%d bytes)", cursor, end-cursor)
		}
		if mapping.Prot&prot != prot {
			return fmt.Errorf("guest protection fault at 0x%08x (%d bytes, prot=0x%x need=0x%x)", cursor, end-cursor, mapping.Prot, prot)
		}
		if mapping.End <= cursor {
			return fmt.Errorf("invalid mapping at 0x%08x", cursor)
		}
		if mapping.End < end {
			cursor = mapping.End
		} else {
			cursor = end
		}
	}
	return nil
}

// IsFree reports whether a guest range has no metadata overlap.
func (a *AddressSpace) IsFree(start, end uint32) bool { return a.isFree(start, end) }

func (a *AddressSpace) isFree(start, end uint32) bool {
	for _, m := range a.Mappings {
		if start < m.End && end > m.Start {
			return false
		}
	}
	return true
}

func (a *AddressSpace) MapAnonymous(addr, size uint32, prot uint8, name string) (uint32, error) {
	if addr == 0 {
		var err error
		addr, err = a.FindHole(size, a.NextHint)
		if err != nil {
			return 0, err
		}
	} else if addr > a.Max || size > a.Max-addr || addr+size < addr {
		return 0, fmt.Errorf("requested mapping outside guest range")
	} else if !a.isFree(addr, addr+size) {
		return 0, fmt.Errorf("requested mapping overlaps existing range")
	}
	if err := a.Add(addr, size, prot, name); err != nil {
		return 0, err
	}
	return addr, nil
}

func (a *AddressSpace) MappingAt(addr uint32) (Mapping, bool) {
	for _, m := range a.Mappings {
		if addr >= m.Start && addr < m.End {
			return m, true
		}
	}
	return Mapping{}, false
}
