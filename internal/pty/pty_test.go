package pty

import (
	"context"
	"io"
	"testing"
	"time"
)

func TestCanonicalInputAndEcho(t *testing.T) {
	term := New()
	if _, err := term.WriteInput([]byte("partial")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	buf := make([]byte, 32)
	if _, err := term.ReadInput(ctx, buf); err == nil {
		t.Fatal("canonical input should wait for newline")
	}
	if _, err := term.WriteInput([]byte(" line\n")); err != nil {
		t.Fatal(err)
	}
	n, err := term.ReadInput(context.Background(), buf)
	if err != nil || string(buf[:n]) != "partial line\n" {
		t.Fatalf("ReadInput = (%q, %v)", buf[:n], err)
	}
	n, err = term.ReadOutput(context.Background(), buf)
	if err != nil || string(buf[:n]) != "partial line\n" {
		t.Fatalf("echo = (%q, %v)", buf[:n], err)
	}
}

func TestCloseUnblocksReads(t *testing.T) {
	term := New()
	term.Close()
	_, err := term.ReadOutput(context.Background(), make([]byte, 1))
	if err != io.EOF {
		t.Fatalf("ReadOutput after Close = %v, want EOF", err)
	}
}

func TestCloseInputSendsEOFOnlyToInput(t *testing.T) {
	term := New()
	if _, err := term.WriteOutput([]byte("out")); err != nil {
		t.Fatal(err)
	}
	term.CloseInput()
	if _, err := term.ReadInput(context.Background(), make([]byte, 1)); err != io.EOF {
		t.Fatalf("ReadInput after CloseInput = %v, want EOF", err)
	}
	buf := make([]byte, 3)
	n, err := term.ReadOutput(context.Background(), buf)
	if err != nil || string(buf[:n]) != "out" {
		t.Fatalf("ReadOutput after CloseInput = (%q, %v)", buf[:n], err)
	}
}
