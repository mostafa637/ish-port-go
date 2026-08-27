package vfs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var ErrNotFound = fs.ErrNotExist

// FS exposes a Linux-like path namespace backed by a private host directory.
// Host paths never escape Root, and virtual files may be supplied by mounts.
type FS struct {
	Root    string
	virtual map[string]func() ([]byte, error)
}

func New(root string) (*FS, error) {
	if root == "" {
		return nil, errors.New("vfs: empty root")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, err
	}
	return &FS{Root: abs, virtual: make(map[string]func() ([]byte, error))}, nil
}

func (f *FS) mountFile(name string, read func() ([]byte, error)) {
	f.virtual[clean(name)] = read
}

func (f *FS) MountDev() {
	f.mountFile("/dev/null", func() ([]byte, error) { return nil, nil })
	f.mountFile("/dev/zero", func() ([]byte, error) { return nil, nil })
	f.mountFile("/dev/tty", func() ([]byte, error) { return nil, nil })
}

func (f *FS) MountProc(version string, pid int) {
	f.mountFile("/proc/version", func() ([]byte, error) {
		return []byte(version + "\n"), nil
	})
	f.mountFile("/proc/self/status", func() ([]byte, error) {
		return []byte(fmt.Sprintf("Name:\tish-go\nPid:\t%d\nState:\tR (running)\n", pid)), nil
	})
	f.mountFile("/proc/uptime", func() ([]byte, error) { return []byte("0.00 0.00\n"), nil })
	f.mountFile("/proc/meminfo", func() ([]byte, error) {
		return []byte("MemTotal:       131072 kB\nMemFree:         65536 kB\nMemAvailable:    65536 kB\nBuffers:             0 kB\nCached:              0 kB\nSwapCached:          0 kB\nActive:              0 kB\nInactive:            0 kB\nSwapTotal:           0 kB\nSwapFree:            0 kB\n"), nil
	})
	f.mountFile("/proc/mounts", func() ([]byte, error) {
		return []byte("rootfs / rootfs rw 0 0\nproc /proc proc rw 0 0\n/dev /dev devtmpfs rw 0 0\n"), nil
	})
}

func (f *FS) resolveLexical(name string) (string, error) {
	if err := validate(name); err != nil {
		return "", err
	}
	name = clean(name)
	rel := strings.TrimPrefix(name, "/")
	host := filepath.Join(f.Root, filepath.FromSlash(rel))
	relCheck, err := filepath.Rel(f.Root, host)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("vfs: path escapes root: %q", name)
	}
	return host, nil
}

func (f *FS) Resolve(name string) (string, error) {
	if err := validate(name); err != nil {
		return "", err
	}
	guest := clean(name)
	for hop := 0; hop < 40; hop++ {
		parts := strings.Split(strings.TrimPrefix(guest, "/"), "/")
		if len(parts) == 1 && parts[0] == "" {
			return f.Root, nil
		}
		resolved := "/"
		restarted := false
		for i, part := range parts {
			candidate := path.Join(resolved, part)
			host, err := f.resolveLexical(candidate)
			if err != nil {
				return "", err
			}
			info, err := os.Lstat(host)
			if err != nil {
				if !errors.Is(err, fs.ErrNotExist) {
					return "", err
				}
				for _, rest := range parts[i+1:] {
					candidate = path.Join(candidate, rest)
				}
				return f.resolveLexical(candidate)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				target, err := os.Readlink(host)
				if err != nil {
					return "", err
				}
				if path.IsAbs(target) {
					guestTarget := clean(target)
					guestHost, hostErr := f.resolveLexical(guestTarget)
					if hostErr != nil {
						return "", hostErr
					}
					if _, guestErr := os.Lstat(guestHost); guestErr == nil {
						resolved = guestTarget
					} else if !errors.Is(guestErr, fs.ErrNotExist) {
						return "", guestErr
					} else if _, outsideErr := os.Lstat(filepath.Clean(target)); outsideErr == nil {
						return "", fmt.Errorf("vfs: symlink escapes root: %q", name)
					} else if !errors.Is(outsideErr, fs.ErrNotExist) {
						return "", outsideErr
					} else {
						resolved = guestTarget
					}
				} else {
					resolved = path.Join(path.Dir(candidate), target)
				}
				for _, rest := range parts[i+1:] {
					resolved = path.Join(resolved, rest)
				}
				guest = clean(resolved)
				restarted = true
				break
			}
			resolved = candidate
		}
		if !restarted {
			return f.resolveLexical(resolved)
		}
	}
	return "", fmt.Errorf("vfs: too many symlink hops: %q", name)
}

func (f *FS) ensureInside(host string) error {
	rel, err := filepath.Rel(f.Root, host)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("vfs: symlink escapes root: %q", host)
	}
	return nil
}

func validate(name string) error {
	for _, part := range strings.Split(strings.ReplaceAll(name, "\\\\", "/"), "/") {
		if part == ".." {
			return fmt.Errorf("vfs: path traversal rejected: %q", name)
		}
	}
	return nil
}

func clean(name string) string {
	if name == "" {
		return "/"
	}
	return "/" + strings.TrimPrefix(path.Clean("/"+name), "/")
}

func (f *FS) Readlink(name string) (string, error) {
	name = clean(name)
	if _, ok := f.virtual[name]; ok {
		return "", fmt.Errorf("vfs: %s is not a symlink", name)
	}
	host, err := f.resolveLexical(name)
	if err != nil {
		return "", err
	}
	return os.Readlink(host)
}

func (f *FS) virtualPath(name string) (string, bool, error) {
	if err := validate(name); err != nil {
		return "", false, err
	}
	guest := clean(name)
	for hop := 0; hop < 40; hop++ {
		if _, ok := f.virtual[guest]; ok {
			return guest, true, nil
		}
		host, err := f.resolveLexical(guest)
		if err != nil {
			return "", false, err
		}
		info, err := os.Lstat(host)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return guest, false, nil
			}
			return "", false, err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return guest, false, nil
		}
		target, err := os.Readlink(host)
		if err != nil {
			return "", false, err
		}
		if path.IsAbs(target) {
			guest = clean(target)
		} else {
			guest = clean(path.Join(path.Dir(guest), target))
		}
	}
	return "", false, fmt.Errorf("vfs: too many symlink hops: %q", name)
}

// ReadVirtual follows guest symlinks and returns data only when the final
// namespace entry is a mounted virtual file. It never reads host paths.
func (f *FS) ReadVirtual(name string) ([]byte, bool, error) {
	resolved, ok, err := f.virtualPath(name)
	if err != nil || !ok {
		return nil, false, err
	}
	read := f.virtual[resolved]
	data, err := read()
	return data, true, err
}

func (f *FS) ReadFile(name string) ([]byte, error) {
	if data, virtual, err := f.ReadVirtual(name); virtual || err != nil {
		return data, err
	}
	host, err := f.Resolve(name)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(host)
}

func (f *FS) WriteFile(name string, data []byte, mode fs.FileMode) error {
	name = clean(name)
	if _, ok := f.virtual[name]; ok {
		return fmt.Errorf("vfs: read-only virtual file %s", name)
	}
	host, err := f.Resolve(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(host), 0o700); err != nil {
		return err
	}
	return os.WriteFile(host, data, mode)
}

func (f *FS) Stat(name string) (fs.FileInfo, error) {
	if data, virtual, err := f.ReadVirtual(name); virtual {
		return virtualInfo{name: path.Base(clean(name)), size: int64(len(data))}, nil
	} else if err != nil {
		return nil, err
	}
	name = clean(name)
	if _, ok := f.virtual[name]; ok {
		return virtualInfo{name: path.Base(name), size: 0}, nil
	}
	prefix := strings.TrimSuffix(name, "/") + "/"
	for virtual := range f.virtual {
		if strings.HasPrefix(virtual, prefix) {
			return virtualInfo{name: path.Base(name), dir: true}, nil
		}
	}
	host, err := f.Resolve(name)
	if err != nil {
		return nil, err
	}
	return os.Stat(host)
}

func (f *FS) List(name string) ([]string, error) {
	name = clean(name)
	host, err := f.Resolve(name)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(host)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	set := make(map[string]bool)
	for _, e := range entries {
		set[e.Name()] = true
	}
	prefix := strings.TrimSuffix(name, "/") + "/"
	for virtual := range f.virtual {
		if strings.HasPrefix(virtual, prefix) {
			rest := strings.TrimPrefix(virtual, prefix)
			if !strings.Contains(rest, "/") && rest != "" {
				set[rest] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for item := range set {
		out = append(out, item)
	}
	sort.Strings(out)
	return out, nil
}

type virtualInfo struct {
	name string
	size int64
	dir  bool
}

func (v virtualInfo) Name() string       { return v.name }
func (v virtualInfo) Size() int64        { return v.size }
func (v virtualInfo) Mode() fs.FileMode  { return 0o444 }
func (v virtualInfo) ModTime() time.Time { return time.Time{} }
func (v virtualInfo) IsDir() bool        { return v.dir }
func (v virtualInfo) Sys() any           { return nil }
