package runtime

import (
	"context"
	"strings"
	"testing"
)

func TestRootShellPOSIXSmoke(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if out, code := s.Submit(context.Background(), "pwd"); out != "/\n" || code != 0 {
		t.Fatalf("pwd=(%q,%d)", out, code)
	}
	if out, code := s.Submit(context.Background(), "ls /proc"); code != 0 || !strings.Contains(out, "version") {
		t.Fatalf("ls /proc=(%q,%d)", out, code)
	}
	if out, code := s.Submit(context.Background(), "cd /proc"); code != 0 || out != "" {
		t.Fatalf("cd /proc=(%q,%d)", out, code)
	}
	if out, code := s.Submit(context.Background(), "cat version"); code != 0 || !strings.Contains(out, "Linux ish-go") {
		t.Fatalf("cat version=(%q,%d)", out, code)
	}
	if out, code := s.Submit(context.Background(), "cat ../../outside"); code == 0 || out == "" {
		t.Fatalf("expected traversal failure, got (%q,%d)", out, code)
	}
}
