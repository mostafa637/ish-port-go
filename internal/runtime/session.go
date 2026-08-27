package runtime

import (
	"context"
	"fmt"
	"io"
	"sync"

	"example.com/ish-go/internal/guest"
	"example.com/ish-go/internal/pty"
	"example.com/ish-go/internal/rootfs"
	"example.com/ish-go/internal/vfs"
)

// Session is the public runtime boundary consumed by the Gio UI.
type Session struct {
	FS        *vfs.FS
	PTY       *pty.Terminal
	Shell     *RootShell
	Scheduler *guest.Scheduler
	mu        sync.Mutex
}

func New(root string) (*Session, error) {
	fsys, err := vfs.New(root)
	if err != nil {
		return nil, err
	}
	tty := pty.New()
	fsys.MountProc("Linux ish-go", 1)
	fsys.MountDev()
	return &Session{FS: fsys, PTY: tty, Shell: NewRootShell(fsys)}, nil
}

func (s *Session) Prompt() string { return s.Shell.Prompt() }

func (s *Session) InstallRootFSGzip(r io.Reader, maxBytes int64) error {
	return rootfs.InstallTarGz(r, s.FS.Root, maxBytes)
}

func (s *Session) Submit(ctx context.Context, line string) (string, int) {
	out, code := s.Shell.Execute(ctx, line)
	if out != "" {
		_, _ = s.PTY.WriteOutput([]byte(out))
	}
	return out, code
}

// RunELF loads and executes a guest i386 image. The instruction budget is
// mandatory for safety: a malformed or unsupported guest cannot hang the UI.
func (s *Session) RunELF(ctx context.Context, path string, argv []string, maxSteps uint64) (int32, error) {
	p, err := guest.LoadWithRuntime(path, argv, s.FS, s.PTY)
	if err != nil {
		return 0, err
	}
	s.Scheduler = guest.NewScheduler(p)
	if err := s.Scheduler.Run(ctx, maxSteps); err != nil {
		return 0, err
	}
	return p.ExitCode, nil
}

// StartGuest starts a long-lived guest process against the session VFS. The
// guest path is resolved inside the VFS, while the ELF loader receives the
// resulting host path only as an implementation detail. The returned channel
// completes when the cooperative scheduler exits or faults.
func (s *Session) StartGuest(ctx context.Context, guestPath string, argv []string, maxSteps uint64) (<-chan error, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	active := s.Scheduler != nil
	s.mu.Unlock()
	if active {
		return nil, fmt.Errorf("runtime: guest process already active")
	}
	hostPath, err := s.FS.Resolve(guestPath)
	if err != nil {
		return nil, err
	}
	p, err := guest.LoadWithRuntime(hostPath, argv, s.FS, s.PTY)
	if err != nil {
		return nil, err
	}
	scheduler := guest.NewScheduler(p)
	s.mu.Lock()
	if s.Scheduler != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("runtime: guest process already active")
	}
	s.Scheduler = scheduler
	s.mu.Unlock()
	done := make(chan error, 1)
	go func() {
		err := scheduler.Run(ctx, maxSteps)
		s.PTY.Close()
		s.mu.Lock()
		if s.Scheduler == scheduler {
			s.Scheduler = nil
		}
		s.mu.Unlock()
		done <- err
	}()
	return done, nil
}

// GuestInput writes bytes to the active guest PTY. It is intentionally
// separate from Submit: Submit remains the built-in RootShell path.
func (s *Session) GuestInput(data []byte) (int, error) {
	s.mu.Lock()
	active := s.Scheduler != nil
	s.mu.Unlock()
	if !active {
		return 0, fmt.Errorf("runtime: no guest process active")
	}
	return s.PTY.WriteInput(data)
}

func (s *Session) String() string {
	return fmt.Sprintf("runtime session root=%s prompt=%q", s.FS.Root, s.Prompt())
}
