package terminal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Backend is the execution boundary between the terminal UI and a shell runtime.
// A future i386/Linux runtime can implement this interface without changing Gio.
type Backend interface {
	Execute(ctx context.Context, line string) (output string, exitCode int)
	Prompt() string
}

// Shell is a small, deterministic, pure-Go runtime for the first port.
// It intentionally uses built-ins instead of os/exec because iOS applications
// cannot spawn arbitrary child processes. The interface is ready for a later
// iSH-compatible i386 emulator backend.
type Shell struct {
	home    string
	cwd     string
	history []string
}

func New() *Shell {
	cwd, err := os.UserHomeDir()
	if err != nil || cwd == "" {
		cwd = "."
	}
	return &Shell{home: cwd, cwd: cwd}
}

func (s *Shell) Prompt() string {
	return "ish-go:" + filepath.Base(s.cwd) + "$ "
}

func (s *Shell) History() []string {
	return append([]string(nil), s.history...)
}

func (s *Shell) Execute(ctx context.Context, line string) (string, int) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", 0
	}
	select {
	case <-ctx.Done():
		return "context canceled\n", 130
	default:
	}
	s.history = append(s.history, line)
	argv, err := lex(line)
	if err != nil {
		return "ish-go: " + err.Error() + "\n", 2
	}
	if len(argv) == 0 {
		return "", 0
	}

	switch argv[0] {
	case "help":
		return "Built-in commands: cat cd clear date echo env exit false help history ls pwd true uname whoami\n", 0
	case "echo":
		return strings.Join(argv[1:], " ") + "\n", 0
	case "pwd":
		return s.cwd + "\n", 0
	case "cd":
		return s.changeDir(argv[1:])
	case "ls":
		return s.list(argv[1:])
	case "cat":
		return s.cat(argv[1:])
	case "env":
		return strings.Join(os.Environ(), "\n") + "\n", 0
	case "uname":
		return "Linux ish-go 6.1.0-go i386\n", 0
	case "whoami":
		return "mobile\n", 0
	case "date":
		return time.Now().Format(time.RFC1123Z) + "\n", 0
	case "history":
		var b strings.Builder
		for i, h := range s.history {
			fmt.Fprintf(&b, "%4d  %s\n", i+1, h)
		}
		return b.String(), 0
	case "true":
		return "", 0
	case "false":
		return "", 1
	case "clear":
		return "\x1b[2J\x1b[H", 0
	case "exit":
		return "Use the close button to leave ish-go.\n", 0
	default:
		return fmt.Sprintf("ish-go: %s: command not found\n", argv[0]), 127
	}
}

func (s *Shell) changeDir(argv []string) (string, int) {
	target := s.cwd
	if len(argv) > 1 {
		return "cd: too many arguments\n", 1
	}
	if len(argv) == 1 && argv[0] != "" {
		target = argv[0]
		if target == "~" {
			target = s.home
		} else if !filepath.IsAbs(target) {
			target = filepath.Join(s.cwd, target)
		}
	}
	info, err := os.Stat(target)
	if err != nil {
		return "cd: " + err.Error() + "\n", 1
	}
	if !info.IsDir() {
		return "cd: not a directory: " + target + "\n", 1
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return "cd: " + err.Error() + "\n", 1
	}
	s.cwd = filepath.Clean(abs)
	return "", 0
}

func (s *Shell) list(argv []string) (string, int) {
	target := s.cwd
	if len(argv) > 1 {
		return "ls: too many arguments\n", 1
	}
	if len(argv) == 1 {
		target = argv[0]
		if !filepath.IsAbs(target) {
			target = filepath.Join(s.cwd, target)
		}
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return "ls: " + err.Error() + "\n", 1
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, "  ") + "\n", 0
}

func (s *Shell) cat(argv []string) (string, int) {
	if len(argv) == 0 {
		return "cat: missing operand\n", 1
	}
	var b strings.Builder
	for _, name := range argv {
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(s.cwd, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "cat: " + err.Error() + "\n", 1
		}
		b.Write(data)
	}
	return b.String(), 0
}

// lex handles whitespace, single/double quotes and backslash escaping.
func lex(line string) ([]string, error) {
	var args []string
	var b strings.Builder
	var quote rune
	escaped := false
	flush := func() {
		if b.Len() > 0 {
			args = append(args, b.String())
			b.Reset()
		}
	}
	for _, r := range line {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
		} else if r == ' ' || r == '\t' || r == '\n' {
			flush()
		} else {
			b.WriteRune(r)
		}
	}
	if escaped {
		return nil, strconv.ErrSyntax
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote")
	}
	flush()
	return args, nil
}
