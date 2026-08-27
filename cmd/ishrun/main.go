package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"example.com/ish-go/internal/guest"
)

func main() {
	root := flag.String("root", ".", "guest filesystem root")
	limit := flag.Uint64("steps", 10_000_000, "maximum guest instructions")
	interactive := flag.Bool("interactive", false, "connect host stdin/stdout to the guest PTY")
	timeout := flag.Duration("timeout", 0, "cancel execution after this duration; zero means no timeout")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: ishrun [-root dir] [-steps n] program [args...]")
		os.Exit(2)
	}
	program := flag.Arg(0)
	argv := flag.Args()
	p, err := guest.Load(program, argv, *root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ishrun:", err)
		os.Exit(1)
	}
	scheduler := guest.NewScheduler(p)
	if *interactive {
		os.Exit(runInteractive(scheduler, p, *limit, *timeout))
	}
	ctx := context.Background()
	cancel := func() {}
	if *timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, *timeout)
	}
	defer cancel()
	if err := scheduler.Run(ctx, *limit); err != nil {
		if output := p.TTY.DrainOutput(); len(output) > 0 {
			_, _ = os.Stdout.Write(output)
		}
		fmt.Fprintln(os.Stderr, "ishrun:", err)
		os.Exit(1)
	}
	if output := p.TTY.DrainOutput(); len(output) > 0 {
		_, _ = os.Stdout.Write(output)
	}
	os.Exit(int(p.ExitCode))
}

func runInteractive(scheduler *guest.Scheduler, p *guest.Process, maxSteps uint64, timeout time.Duration) int {
	ctx := context.Background()
	cancel := func() {}
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- scheduler.Run(ctx, maxSteps)
		p.TTY.Close()
	}()
	go func() {
		_, _ = io.Copy(terminalInputWriter{tty: p.TTY}, os.Stdin)
		p.TTY.CloseInput()
	}()

	buf := make([]byte, 4096)
	for {
		n, err := p.TTY.ReadOutput(ctx, buf)
		if n > 0 {
			_, _ = os.Stdout.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	if err := <-done; err != nil {
		fmt.Fprintln(os.Stderr, "ishrun:", err)
		return 1
	}
	return int(p.ExitCode)
}

type terminalInputWriter struct {
	tty interface{ WriteInput([]byte) (int, error) }
}

func (w terminalInputWriter) Write(data []byte) (int, error) { return w.tty.WriteInput(data) }
