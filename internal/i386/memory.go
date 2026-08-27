package i386

import (
	"encoding/binary"
	"fmt"
)

// Memory is a bounded little-endian guest address space. Guest addresses are
// offsets into this byte slice and are never exposed as host pointers.
type Memory struct {
	data      []byte
	space     *AddressSpace
	protected bool
}

func NewMemory(size uint32) *Memory {
	if size == 0 {
		size = 1
	}
	return &Memory{data: make([]byte, size)}
}

func (m *Memory) Size() uint32 { return uint32(len(m.data)) }

func (m *Memory) check(addr uint32, width uint32) error {
	if addr > uint32(len(m.data)) || width > uint32(len(m.data))-addr {
		return fmt.Errorf("guest memory fault at 0x%08x (%d bytes)", addr, width)
	}
	return nil
}

func (m *Memory) checkAccess(addr, width uint32, prot uint8) error {
	if err := m.check(addr, width); err != nil {
		return err
	}
	if !m.protected || width == 0 {
		return nil
	}
	if err := m.space.checkRange(addr, width, prot); err != nil {
		return err
	}
	return nil
}

// EnableProtection attaches mapping permissions to this memory. It is called
// after ELF/stack initialization so loader writes remain initialization-only.
func (m *Memory) EnableProtection(space *AddressSpace) {
	m.space = space
	m.protected = space != nil
}

// ReadRaw and WriteRaw bypass guest page permissions for kernel/loader
// initialization. Guest CPU accesses must use Read/Write/Fetch instead.
func (m *Memory) ReadRaw(addr, size uint32) ([]byte, error) {
	if err := m.check(addr, size); err != nil {
		return nil, err
	}
	out := make([]byte, size)
	copy(out, m.data[addr:addr+size])
	return out, nil
}

func (m *Memory) WriteRaw(addr uint32, value []byte) error {
	if err := m.check(addr, uint32(len(value))); err != nil {
		return err
	}
	copy(m.data[addr:], value)
	return nil
}

func (m *Memory) Fetch8(addr uint32) (byte, error) {
	if err := m.checkAccess(addr, 1, ProtExec); err != nil {
		return 0, err
	}
	return m.data[addr], nil
}

func (m *Memory) Read8(addr uint32) (byte, error) {
	if err := m.checkAccess(addr, 1, ProtRead); err != nil {
		return 0, err
	}
	return m.data[addr], nil
}

func (m *Memory) Read16(addr uint32) (uint16, error) {
	if err := m.checkAccess(addr, 2, ProtRead); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(m.data[addr:]), nil
}

func (m *Memory) Read32(addr uint32) (uint32, error) {
	if err := m.checkAccess(addr, 4, ProtRead); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(m.data[addr:]), nil
}

func (m *Memory) ReadBytes(addr, size uint32) ([]byte, error) {
	if err := m.checkAccess(addr, size, ProtRead); err != nil {
		return nil, err
	}
	out := make([]byte, size)
	copy(out, m.data[addr:addr+size])
	return out, nil
}

func (m *Memory) Write8(addr uint32, value byte) error {
	if err := m.checkAccess(addr, 1, ProtWrite); err != nil {
		return err
	}
	m.data[addr] = value
	return nil
}

func (m *Memory) Write16(addr uint32, value uint16) error {
	if err := m.checkAccess(addr, 2, ProtWrite); err != nil {
		return err
	}
	binary.LittleEndian.PutUint16(m.data[addr:], value)
	return nil
}

func (m *Memory) Write32(addr uint32, value uint32) error {
	if err := m.checkAccess(addr, 4, ProtWrite); err != nil {
		return err
	}
	binary.LittleEndian.PutUint32(m.data[addr:], value)
	return nil
}

func (m *Memory) WriteBytes(addr uint32, value []byte) error {
	if err := m.checkAccess(addr, uint32(len(value)), ProtWrite); err != nil {
		return err
	}
	copy(m.data[addr:], value)
	return nil
}

func (m *Memory) ReadCString(addr, max uint32) (string, error) {
	if max == 0 {
		return "", nil
	}
	if err := m.check(addr, 1); err != nil {
		return "", err
	}
	end := addr
	limit := uint32(len(m.data))
	if max < limit-addr {
		limit = addr + max
	}
	for end < limit {
		value, err := m.Read8(end)
		if err != nil {
			return "", err
		}
		if value == 0 {
			break
		}
		end++
	}
	if end == limit {
		return "", fmt.Errorf("guest string at 0x%08x is not terminated", addr)
	}
	return string(m.data[addr:end]), nil
}
