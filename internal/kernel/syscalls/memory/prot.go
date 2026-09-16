package memory

import "example.com/ish-go/internal/i386"

func Prot(flags uint32) uint8 {
	var p uint8
	if flags&1 != 0 {
		p |= i386.ProtRead
	}
	if flags&2 != 0 {
		p |= i386.ProtWrite
	}
	if flags&4 != 0 {
		p |= i386.ProtExec
	}
	return p
}
