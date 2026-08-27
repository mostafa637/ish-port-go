package runtime

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"example.com/ish-go/internal/vfs"
)

// RootShell is the in-process command runtime exposed to Gio. Its paths are
// guest paths and all data access goes through VFS.
type RootShell struct {
	fs      *vfs.FS
	cwd     string
	history []string
}

func NewRootShell(fsys *vfs.FS) *RootShell { return &RootShell{fs: fsys, cwd: "/"} }

func (s *RootShell) Prompt() string {
	name := path.Base(s.cwd)
	if name == "." || name == "/" {
		name = "root"
	}
	return "ish-go:" + name + "$ "
}

func (s *RootShell) Execute(ctx context.Context, line string) (string, int) {
	select {
	case <-ctx.Done():
		return "context canceled\n", 130
	default:
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return "", 0
	}
	s.history = append(s.history, line)
	argv, err := parseWords(line)
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
		return s.cd(argv[1:])
	case "ls":
		return s.ls(argv[1:])
	case "cat":
		return s.cat(argv[1:])
	case "env":
		return "HOME=/root\nTERM=xterm-256color\nPATH=/bin:/usr/bin\n", 0
	case "uname":
		return "Linux ish-go 6.1.0-go i386\n", 0
	case "whoami":
		return "root\n", 0
	case "date":
		return time.Now().Format(time.RFC1123Z) + "\n", 0
	case "history":
		var b strings.Builder
		for i, command := range s.history {
			fmt.Fprintf(&b, "%4d  %s\n", i+1, command)
		}
		return b.String(), 0
	case "clear":
		return "\x1b[2J\x1b[H", 0
	case "true":
		return "", 0
	case "false":
		return "", 1
	case "exit":
		return "Use the close button to leave ish-go.\n", 0
	default:
		return fmt.Sprintf("ish-go: %s: command not found\n", argv[0]), 127
	}
}

func (s *RootShell) cd(argv []string) (string, int) {
	if len(argv) > 1 {
		return "cd: too many arguments\n", 1
	}
	target := s.cwd
	if len(argv) == 1 {
		if argv[0] == "~" {
			target = "/root"
		} else if path.IsAbs(argv[0]) {
			target = path.Clean(argv[0])
		} else {
			target = path.Join(s.cwd, argv[0])
		}
	}
	info, err := s.fs.Stat(target)
	if err != nil || !info.IsDir() {
		return "cd: no such directory: " + target + "\n", 1
	}
	s.cwd = path.Clean(target)
	return "", 0
}

func (s *RootShell) ls(argv []string) (string, int) {
	if len(argv) > 1 {
		return "ls: too many arguments\n", 1
	}
	target := s.cwd
	if len(argv) == 1 {
		target = s.guestPath(argv[0])
	}
	names, err := s.fs.List(target)
	if err != nil {
		return "ls: " + err.Error() + "\n", 1
	}
	return strings.Join(names, "  ") + "\n", 0
}

func (s *RootShell) cat(argv []string) (string, int) {
	if len(argv) == 0 {
		return "cat: missing operand\n", 1
	}
	var b strings.Builder
	for _, name := range argv {
		data, err := s.fs.ReadFile(s.guestPath(name))
		if err != nil {
			return "cat: " + err.Error() + "\n", 1
		}
		b.Write(data)
	}
	return b.String(), 0
}

func (s *RootShell) guestPath(name string) string {
	if path.IsAbs(name) {
		return path.Clean(name)
	}
	return path.Join(s.cwd, name)
}

func parseWords(line string) ([]string, error) {
	var out []string
	var b strings.Builder
	var quote byte
	escaped := false
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if escaped {
			b.WriteByte(ch)
			escaped = false
			continue
		}
		if ch == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			} else {
				b.WriteByte(ch)
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
		} else if ch == ' ' || ch == '\t' || ch == '\n' {
			flush()
		} else {
			b.WriteByte(ch)
		}
	}
	if escaped || quote != 0 {
		return nil, fmt.Errorf("unterminated escape or quote")
	}
	flush()
	return out, nil
}
