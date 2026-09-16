package kernel

import (
	"example.com/ish-go/internal/syscalls/filesystem"
	"example.com/ish-go/internal/syscalls/io"
	"example.com/ish-go/internal/syscalls/memory"
	"example.com/ish-go/internal/syscalls/process"
	"example.com/ish-go/internal/syscalls/signals"
)

const (
	_ = filesystem.NumberOpen
	_ = io.NumberRead
	_ = memory.NumberMmap2
	_ = process.NumberClone
	_ = signals.NumberSigaction
)
