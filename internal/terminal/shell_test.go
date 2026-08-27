package terminal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLexQuotesAndEscapes(t *testing.T) {
	got, err := lex(`echo "hello world" 'from shell' escaped\ value`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"echo", "hello world", "from shell", "escaped value"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("lex() = %#v, want %#v", got, want)
	}
}

func TestLexRejectsUnterminatedInput(t *testing.T) {
	if _, err := lex(`echo "unterminated`); err == nil {
		t.Fatal("expected unterminated quote error")
	}
}

func TestBuiltins(t *testing.T) {
	s := New()
	for _, tc := range []struct {
		name string
		line string
		want string
		code int
	}{
		{"echo", `echo "hello world"`, "hello world\n", 0},
		{"pwd", "pwd", s.cwd + "\n", 0},
		{"uname", "uname", "Linux ish-go 6.1.0-go i386\n", 0},
		{"false", "false", "", 1},
		{"unknown", "not-a-command", "ish-go: not-a-command: command not found\n", 127},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, code := s.Execute(context.Background(), tc.line)
			if got != tc.want || code != tc.code {
				t.Fatalf("Execute(%q) = (%q, %d), want (%q, %d)", tc.line, got, code, tc.want, tc.code)
			}
		})
	}
}

func TestChangeDirectoryAndList(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New()
	if _, code := s.Execute(context.Background(), "cd "+root); code != 0 {
		t.Fatalf("cd failed with code %d", code)
	}
	out, code := s.Execute(context.Background(), "ls")
	if code != 0 || !strings.Contains(out, "hello.txt") {
		t.Fatalf("ls = (%q, %d)", out, code)
	}
	out, code = s.Execute(context.Background(), "cat hello.txt")
	if code != 0 || out != "hello\n" {
		t.Fatalf("cat = (%q, %d)", out, code)
	}
}

func TestCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, code := New().Execute(ctx, "pwd")
	if code != 130 || out != "context canceled\n" {
		t.Fatalf("canceled Execute = (%q, %d)", out, code)
	}
}
