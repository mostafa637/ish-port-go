package pty

import (
	"context"
	"io"
	"sync"
)

// Terminal is an in-process PTY endpoint. It never allocates a host process
// and is therefore suitable for an iOS sandbox.
type Terminal struct {
	mu          sync.Mutex
	input       []byte
	output      []byte
	inputWake   chan struct{}
	outputWake  chan struct{}
	closed      bool
	inputClosed bool
	Canonical   bool
	Echo        bool
}

func New() *Terminal {
	return &Terminal{inputWake: make(chan struct{}, 1), outputWake: make(chan struct{}, 1), Canonical: true, Echo: true}
}

func (t *Terminal) signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (t *Terminal) Close() {
	t.mu.Lock()
	t.closed = true
	t.inputClosed = true
	t.mu.Unlock()
	t.signal(t.inputWake)
	t.signal(t.outputWake)
}

// CloseInput sends EOF to readers while keeping output available for draining.
func (t *Terminal) CloseInput() {
	t.mu.Lock()
	t.inputClosed = true
	t.mu.Unlock()
	t.signal(t.inputWake)
}

func (t *Terminal) WriteInput(data []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.inputClosed {
		return 0, io.EOF
	}
	t.input = append(t.input, data...)
	if t.Echo {
		t.output = append(t.output, data...)
		t.signal(t.outputWake)
	}
	t.signal(t.inputWake)
	return len(data), nil
}

func (t *Terminal) WriteOutput(data []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return 0, io.EOF
	}
	t.output = append(t.output, data...)
	t.signal(t.outputWake)
	return len(data), nil
}

func (t *Terminal) ReadOutput(ctx context.Context, dst []byte) (int, error) {
	return t.read(ctx, dst, false)
}

func (t *Terminal) ReadInput(ctx context.Context, dst []byte) (int, error) {
	return t.read(ctx, dst, true)
}

// InputReady reports whether a non-blocking read would return data or EOF.
// Canonical terminals become ready only after a newline/carriage return, just
// like the line discipline used by the guest shell.
func (t *Terminal) InputReady() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.inputReadyLocked()
}

// WaitInput waits without consuming input until a read would make progress.
func (t *Terminal) WaitInput(ctx context.Context) bool {
	for {
		t.mu.Lock()
		ready := t.inputReadyLocked()
		wake := t.inputWake
		t.mu.Unlock()
		if ready {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-wake:
		}
	}
}

func (t *Terminal) inputReadyLocked() bool {
	return t.closed || t.inputClosed || (len(t.input) > 0 && (!t.Canonical || containsNewline(t.input)))
}

func (t *Terminal) read(ctx context.Context, dst []byte, input bool) (int, error) {
	if len(dst) == 0 {
		return 0, nil
	}
	for {
		t.mu.Lock()
		queue := &t.output
		wake := t.outputWake
		if input {
			queue = &t.input
			wake = t.inputWake
		}
		ready := len(*queue) > 0
		if ready && (!input || !t.Canonical || containsNewline(*queue)) {
			n := copy(dst, *queue)
			*queue = (*queue)[n:]
			t.mu.Unlock()
			return n, nil
		}
		closed := t.closed || (input && t.inputClosed)
		t.mu.Unlock()
		if closed {
			return 0, io.EOF
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-wake:
		}
	}
}

func containsNewline(data []byte) bool {
	for _, b := range data {
		if b == '\n' || b == '\r' {
			return true
		}
	}
	return false
}

func (t *Terminal) DrainOutput() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := append([]byte(nil), t.output...)
	t.output = nil
	return out
}
