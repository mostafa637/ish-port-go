package kernel

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"syscall"
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

const pipeCapacity = 64 * 1024

var errPipeWouldBlock = errors.New("pipe would block")

type pipeBuffer struct {
	mu        sync.Mutex
	data      []byte
	readers   int
	writers   int
	readWake  chan struct{}
	writeWake chan struct{}
}

func newPipePair(nonblock bool) (*pipeHandle, *pipeHandle) {
	p := &pipeBuffer{readers: 1, writers: 1, readWake: make(chan struct{}, 1), writeWake: make(chan struct{}, 1)}
	return &pipeHandle{pipe: p, nonblock: nonblock}, &pipeHandle{pipe: p, write: true, nonblock: nonblock}
}

func (p *pipeBuffer) signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

type pipeHandle struct {
	pipe     *pipeBuffer
	write    bool
	nonblock bool
	closed   bool
}

func (h *pipeHandle) CloneHandle() fileHandle {
	h.pipe.mu.Lock()
	if h.write {
		h.pipe.writers++
	} else {
		h.pipe.readers++
	}
	h.pipe.mu.Unlock()
	return &pipeHandle{pipe: h.pipe, write: h.write, nonblock: h.nonblock}
}

func (h *pipeHandle) Read(dst []byte) (int, error) {
	if h.write {
		return 0, os.ErrInvalid
	}
	if len(dst) == 0 {
		return 0, nil
	}
	for {
		h.pipe.mu.Lock()
		if len(h.pipe.data) > 0 {
			n := copy(dst, h.pipe.data)
			h.pipe.data = h.pipe.data[n:]
			h.pipe.signal(h.pipe.writeWake)
			h.pipe.mu.Unlock()
			return n, nil
		}
		if h.pipe.writers == 0 {
			h.pipe.mu.Unlock()
			return 0, io.EOF
		}
		wake := h.pipe.readWake
		h.pipe.mu.Unlock()
		if h.nonblock {
			return 0, syscall.EAGAIN
		}
		<-wake
	}
}

func (h *pipeHandle) ReadNonblocking(dst []byte) (int, error) {
	if h.write {
		return 0, os.ErrInvalid
	}
	if len(dst) == 0 {
		return 0, nil
	}
	h.pipe.mu.Lock()
	defer h.pipe.mu.Unlock()
	if len(h.pipe.data) > 0 {
		n := copy(dst, h.pipe.data)
		h.pipe.data = h.pipe.data[n:]
		h.pipe.signal(h.pipe.writeWake)
		return n, nil
	}
	if h.pipe.writers == 0 {
		return 0, io.EOF
	}
	return 0, errPipeWouldBlock
}

func (h *pipeHandle) WriteNonblocking(src []byte) (int, error) {
	if !h.write {
		return 0, os.ErrInvalid
	}
	if len(src) == 0 {
		return 0, nil
	}
	h.pipe.mu.Lock()
	defer h.pipe.mu.Unlock()
	if h.pipe.readers == 0 {
		return 0, syscall.EPIPE
	}
	space := pipeCapacity - len(h.pipe.data)
	if space == 0 {
		return 0, errPipeWouldBlock
	}
	n := len(src)
	if n > space {
		n = space
	}
	h.pipe.data = append(h.pipe.data, src[:n]...)
	h.pipe.signal(h.pipe.readWake)
	return n, nil
}

func (h *pipeHandle) Write(src []byte) (int, error) {
	if !h.write {
		return 0, os.ErrInvalid
	}
	written := 0
	for written < len(src) {
		h.pipe.mu.Lock()
		if h.pipe.readers == 0 {
			h.pipe.mu.Unlock()
			if written > 0 {
				return written, syscall.EPIPE
			}
			return 0, syscall.EPIPE
		}
		space := pipeCapacity - len(h.pipe.data)
		if space > 0 {
			n := len(src) - written
			if n > space {
				n = space
			}
			h.pipe.data = append(h.pipe.data, src[written:written+n]...)
			written += n
			h.pipe.signal(h.pipe.readWake)
			h.pipe.mu.Unlock()
			continue
		}
		wake := h.pipe.writeWake
		h.pipe.mu.Unlock()
		if h.nonblock {
			if written > 0 {
				return written, syscall.EAGAIN
			}
			return 0, syscall.EAGAIN
		}
		<-wake
	}
	return written, nil
}

func (h *pipeHandle) Seek(int64, int) (int64, error) { return 0, os.ErrInvalid }

func (h *pipeHandle) Close() error {
	if h.closed {
		return nil
	}
	h.closed = true
	h.pipe.mu.Lock()
	if h.write {
		h.pipe.writers--
		h.pipe.signal(h.pipe.readWake)
	} else {
		h.pipe.readers--
		h.pipe.signal(h.pipe.writeWake)
	}
	h.pipe.mu.Unlock()
	return nil
}

func (h *pipeHandle) Stat() (os.FileInfo, error) {
	return deviceInfo{name: "pipe", mode: os.ModeNamedPipe | 0o660}, nil
}
func (*pipeHandle) Readdirnames(int) ([]string, error) { return nil, os.ErrInvalid }

func (h *pipeHandle) ReadReady() bool {
	h.pipe.mu.Lock()
	defer h.pipe.mu.Unlock()
	return !h.write && (len(h.pipe.data) > 0 || h.pipe.writers == 0)
}

func (h *pipeHandle) WriteReady() bool {
	h.pipe.mu.Lock()
	defer h.pipe.mu.Unlock()
	return h.write && h.pipe.readers > 0 && len(h.pipe.data) < pipeCapacity
}

func (h *pipeHandle) Hangup() bool {
	h.pipe.mu.Lock()
	defer h.pipe.mu.Unlock()
	if h.write {
		return h.pipe.readers == 0
	}
	return h.pipe.writers == 0
}
