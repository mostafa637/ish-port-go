package memory

import "example.com/ish-go/internal/i386"

func Munmap(space *i386.AddressSpace, addr, length uint32) int32 {
	size, ok := PageLength(length)
	if space == nil || !ok || addr&0xfff != 0 {
		return -22
	}
	if err := space.Remove(addr, size); err != nil {
		return -22
	}
	return 0
}
