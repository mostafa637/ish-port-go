package rootfs

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const DefaultMaxBytes int64 = 512 << 20

// InstallTarGz extracts a rootfs into root without following entries outside it.
// Symlinks are created only as archive metadata; callers may disable them by
// validating the resulting tree before exposing it to the guest.
func InstallTarGz(r io.Reader, root string, maxBytes int64) error {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("rootfs gzip: %w", err)
	}
	defer gz.Close()
	return InstallTar(gz, root, maxBytes)
}

func InstallTar(r io.Reader, root string, maxBytes int64) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(absRoot, 0o700); err != nil {
		return err
	}
	total := int64(0)
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("rootfs tar: %w", err)
		}
		name := filepath.Clean(filepath.FromSlash(h.Name))
		if name == "." || filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return fmt.Errorf("rootfs: unsafe entry %q", h.Name)
		}
		dest := filepath.Join(absRoot, name)
		rel, err := filepath.Rel(absRoot, dest)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("rootfs: entry escapes root: %q", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dest, os.FileMode(h.Mode)&0o777); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if h.Size < 0 || total+h.Size > maxBytes {
				return fmt.Errorf("rootfs: file data limit exceeded")
			}
			total += h.Size
			if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
				return err
			}
			f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode)&0o777)
			if err != nil {
				return err
			}
			_, copyErr := io.CopyN(f, tr, h.Size)
			closeErr := f.Close()
			if copyErr != nil && copyErr != io.EOF {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
				return err
			}
			_ = os.Remove(dest)
			if err := os.Symlink(h.Linkname, dest); err != nil {
				return err
			}
		case tar.TypeLink:
			link := filepath.Join(absRoot, filepath.Clean(filepath.FromSlash(h.Linkname)))
			if err := os.Link(link, dest); err != nil {
				return err
			}
		default:
			// Devices, FIFOs, and extended metadata are intentionally ignored;
			// they are provided by VFS special mounts inside the guest.
		}
	}
}
