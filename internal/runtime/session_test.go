package runtime

import (
	"context"
	"testing"
)

func TestSessionSubmitAndPTY(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	out, code := s.Submit(context.Background(), `echo "hello runtime"`)
	if code != 0 || out != "hello runtime\n" {
		t.Fatalf("Submit = (%q, %d)", out, code)
	}
	buf := make([]byte, 64)
	n, err := s.PTY.ReadOutput(context.Background(), buf)
	if err != nil || string(buf[:n]) != "hello runtime\n" {
		t.Fatalf("PTY output = (%q, %v)", buf[:n], err)
	}
}
