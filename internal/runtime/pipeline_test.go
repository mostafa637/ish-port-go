//go:build !race

package runtime

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunGuestPipelineThroughPipe(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "alpine-x86")
	if _, err := os.Stat(root); err != nil {
		t.Skipf("Alpine x86 rootfs not present: %v", err)
	}
	session, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	code, err := session.RunELF(ctx, filepath.Join(root, "bin", "busybox"), []string{"/bin/busybox", "sh", "-c", "echo pipe-ok | wc -c"}, 250_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("pipeline exit code=%d", code)
	}
	if output := session.PTY.DrainOutput(); !bytes.Contains(output, []byte("8")) {
		t.Fatalf("pipeline output=%q", output)
	}
}
