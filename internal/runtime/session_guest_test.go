package runtime

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStartGuestInteractiveInput(t *testing.T) {
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
	done, err := session.StartGuest(ctx, "/bin/busybox", []string{"/bin/busybox", "sh", "-i"}, 200_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.GuestInput([]byte("echo session-ok\nexit\n")); err != nil {
		t.Fatal(err)
	}
	outputDone := make(chan []byte, 1)
	go func() {
		var output bytes.Buffer
		buf := make([]byte, 4096)
		for {
			n, readErr := session.PTY.ReadOutput(ctx, buf)
			if n > 0 {
				output.Write(buf[:n])
			}
			if readErr != nil {
				outputDone <- output.Bytes()
				return
			}
		}
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("interactive guest run: %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case output := <-outputDone:
		if !bytes.Contains(output, []byte("session-ok")) {
			t.Fatalf("interactive output=%q", output)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestStartGuestRunsBusyBox(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "alpine-x86")
	if _, err := os.Stat(root); err != nil {
		t.Skipf("Alpine x86 rootfs not present: %v", err)
	}
	session, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	done, err := session.StartGuest(ctx, "/bin/busybox", []string{"/bin/busybox", "true"}, 100_000_000)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("guest run: %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := session.GuestInput([]byte("ignored\n")); err == nil {
		t.Fatal("GuestInput succeeded after guest termination")
	}
}
