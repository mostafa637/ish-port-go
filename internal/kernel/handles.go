package kernel

import (
	"context"
	"io"
	"os"
	"time"

	"example.com/ish-go/internal/pty"
)

type fileHandle interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Seek(int64, int) (int64, error)
	Close() error
	Stat() (os.FileInfo, error)
	Readdirnames(int) ([]string, error)
}

type deviceInfo struct {
	name string
	mode os.FileMode
}

func (i deviceInfo) Name() string { return i.name }
func (i deviceInfo) Size() int64  { return 0 }
func (i deviceInfo) Mode() os.FileMode {
	if i.mode != 0 {
		return i.mode
	}
	return 0o666
}
func (i deviceInfo) ModTime() time.Time { return time.Time{} }
func (i deviceInfo) IsDir() bool        { return false }
func (i deviceInfo) Sys() any           { return nil }

type virtualHandle struct {
	name   string
	data   []byte
	offset int64
}

func (h *virtualHandle) Read(dst []byte) (int, error) {
	if h.offset >= int64(len(h.data)) {
		return 0, io.EOF
	}
	n := copy(dst, h.data[h.offset:])
	h.offset += int64(n)
	return n, nil
}
func (*virtualHandle) Write([]byte) (int, error) { return 0, os.ErrPermission }
func (h *virtualHandle) Seek(offset int64, whence int) (int64, error) {
	base := int64(0)
	switch whence {
	case 0:
	case 1:
		base = h.offset
	case 2:
		base = int64(len(h.data))
	default:
		return 0, os.ErrInvalid
	}
	if base+offset < 0 {
		return 0, os.ErrInvalid
	}
	h.offset = base + offset
	return h.offset, nil
}
func (h *virtualHandle) Close() error { return nil }
func (h *virtualHandle) Stat() (os.FileInfo, error) {
	return regularInfo{name: h.name, size: int64(len(h.data))}, nil
}
func (*virtualHandle) Readdirnames(int) ([]string, error) { return nil, os.ErrInvalid }

type regularInfo struct {
	name string
	size int64
}

func (i regularInfo) Name() string      { return i.name }
func (i regularInfo) Size() int64       { return i.size }
func (i regularInfo) Mode() os.FileMode { return 0o444 }
func (regularInfo) ModTime() time.Time  { return time.Time{} }
func (regularInfo) IsDir() bool         { return false }
func (regularInfo) Sys() any            { return nil }

type nullHandle struct{}

func (nullHandle) Read([]byte) (int, error)       { return 0, io.EOF }
func (nullHandle) Write(data []byte) (int, error) { return len(data), nil }
func (nullHandle) Seek(int64, int) (int64, error) { return 0, os.ErrInvalid }
func (nullHandle) Close() error                   { return nil }
func (nullHandle) Stat() (os.FileInfo, error) {
	return deviceInfo{name: "null", mode: os.ModeCharDevice | 0o666}, nil
}
func (nullHandle) Readdirnames(int) ([]string, error) { return nil, os.ErrInvalid }

type zeroHandle struct{}

func (zeroHandle) Read(data []byte) (int, error) {
	for i := range data {
		data[i] = 0
	}
	return len(data), nil
}
func (zeroHandle) Write(data []byte) (int, error) { return len(data), nil }
func (zeroHandle) Seek(int64, int) (int64, error) { return 0, os.ErrInvalid }
func (zeroHandle) Close() error                   { return nil }
func (zeroHandle) Stat() (os.FileInfo, error) {
	return deviceInfo{name: "zero", mode: os.ModeCharDevice | 0o666}, nil
}
func (zeroHandle) Readdirnames(int) ([]string, error) { return nil, os.ErrInvalid }

type ttyHandle struct{ tty *pty.Terminal }

func (h ttyHandle) Read(data []byte) (int, error) {
	return h.tty.ReadInput(context.Background(), data)
}
func (h ttyHandle) Write(data []byte) (int, error) { return h.tty.WriteOutput(data) }
func (h ttyHandle) Seek(int64, int) (int64, error) { return 0, os.ErrInvalid }
func (h ttyHandle) Close() error                   { return nil }
func (h ttyHandle) Stat() (os.FileInfo, error) {
	return deviceInfo{name: "tty", mode: os.ModeCharDevice | 0o666}, nil
}
func (h ttyHandle) Readdirnames(int) ([]string, error) { return nil, os.ErrInvalid }
