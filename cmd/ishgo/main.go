package main

import (
	"context"
	"errors"
	"image/color"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"gioui.org/app"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	ishruntime "example.com/ish-go/internal/runtime"
)

type C = layout.Context
type D = layout.Dimensions

var (
	background = color.NRGBA{R: 13, G: 17, B: 23, A: 255}
	panel      = color.NRGBA{R: 22, G: 27, B: 34, A: 255}
	green      = color.NRGBA{R: 126, G: 231, B: 135, A: 255}
	muted      = color.NRGBA{R: 139, G: 148, B: 158, A: 255}
)

type line struct {
	text  string
	color color.NRGBA
}

type terminalView struct {
	theme       *material.Theme
	session     *ishruntime.Session
	guestMode   bool
	guestDone   <-chan error
	guestCancel context.CancelFunc
	input       widget.Editor
	run         widget.Clickable
	clear       widget.Clickable
	output      widget.List
	lines       []line
	mu          sync.Mutex
	pending     atomic.Bool
}

func newTerminalView() *terminalView {
	th := material.NewTheme()
	th.Palette.Bg = background
	th.Palette.Fg = green
	th.Palette.ContrastBg = panel
	th.Palette.ContrastFg = green
	th.Palette.Fg = green
	root := os.Getenv("ISHGO_ROOT")
	if root == "" {
		home, homeErr := os.UserHomeDir()
		if homeErr == nil {
			root = home
		}
		if root == "" {
			root = "."
		}
	}
	session, err := ishruntime.New(root)
	if err != nil {
		panic(err)
	}
	v := &terminalView{theme: th, session: session, guestMode: os.Getenv("ISHGO_GUEST") == "1"}
	v.input.SingleLine = true
	if v.guestMode {
		v.lines = []line{{text: "iSH Go — guest BusyBox mode", color: green}, {text: "ISHGO_GUEST=1: input is sent to /bin/busybox sh -i", color: muted}, {text: "", color: green}}
		v.startGuest()
	}
	v.input.Submit = true
	v.input.InputHint = key.HintAny
	v.output.Axis = layout.Vertical
	v.output.ScrollToEnd = true
	v.lines = []line{
		{text: "iSH Go — Pure Go + Gio", color: green},
		{text: "Type help for built-in commands.", color: muted},
		{text: "", color: green},
	}
	return v
}

func (v *terminalView) append(text string, c color.NRGBA) {
	v.mu.Lock()
	defer v.mu.Unlock()
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	for _, part := range strings.Split(text, "\n") {
		v.lines = append(v.lines, line{text: part, color: c})
	}
}

func (v *terminalView) submit() {
	cmd := strings.TrimSpace(v.input.Text())
	if cmd == "" || v.pending.Load() {
		return
	}
	v.input.SetText("")
	if v.guestMode {
		if _, err := v.session.GuestInput([]byte(cmd + "\n")); err != nil {
			v.append("guest input: "+err.Error(), color.NRGBA{R: 255, G: 123, B: 114, A: 255})
		}
		return
	}
	v.append(v.session.Prompt()+cmd, green)
	v.pending.Store(true)
	go func() {
		out, code := v.session.Submit(context.Background(), cmd)
		if out == "\x1b[2J\x1b[H" {
			v.mu.Lock()
			v.lines = []line{}
			v.mu.Unlock()
		} else if out != "" {
			c := green
			if code != 0 {
				c = color.NRGBA{R: 255, G: 123, B: 114, A: 255}
			}
			v.append(out, c)
		}
		v.pending.Store(false)
		window.Invalidate()
	}()
}

func (v *terminalView) startGuest() {
	ctx, cancel := context.WithCancel(context.Background())
	v.guestCancel = cancel
	done, err := v.session.StartGuest(ctx, "/bin/busybox", []string{"/bin/busybox", "sh", "-i"}, 250_000_000)
	if err != nil {
		v.append("guest start: "+err.Error(), color.NRGBA{R: 255, G: 123, B: 114, A: 255})
		cancel()
		return
	}
	v.guestDone = done
	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := v.session.PTY.ReadOutput(ctx, buf)
			if n > 0 {
				v.append(string(buf[:n]), green)
				window.Invalidate()
			}
			if readErr != nil {
				return
			}
		}
	}()
	go func() {
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			v.append("guest: "+err.Error(), color.NRGBA{R: 255, G: 123, B: 114, A: 255})
		}
		window.Invalidate()
	}()
}

func fill(gtx C, c color.NRGBA) D {
	paint.FillShape(gtx.Ops, c, clip.Rect{Max: gtx.Constraints.Max}.Op())
	return D{Size: gtx.Constraints.Max}
}

func (v *terminalView) Layout(gtx C) D {
	return layout.Stack{}.Layout(gtx,
		layout.Stacked(func(gtx C) D {
			return fill(gtx, background)
		}),
		layout.Expanded(func(gtx C) D {
			return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return material.H6(v.theme, "iSH Go").Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
					layout.Flexed(1, func(gtx C) D {
						return v.layoutOutput(gtx)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Flexed(1, func(gtx C) D {
								return material.Editor(v.theme, &v.input, "command").Layout(gtx)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
							layout.Rigid(func(gtx C) D {
								if v.run.Clicked(gtx) {
									v.submit()
								}
								return material.Button(v.theme, &v.run, "Run").Layout(gtx)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
							layout.Rigid(func(gtx C) D {
								if v.clear.Clicked(gtx) {
									v.mu.Lock()
									v.lines = []line{}
									v.mu.Unlock()
									gtx.Execute(op.InvalidateCmd{})
								}
								return material.Button(v.theme, &v.clear, "Clear").Layout(gtx)
							}),
						)
					}),
				)
			})
		}),
	)
}

func (v *terminalView) layoutOutput(gtx C) D {
	v.mu.Lock()
	lines := append([]line(nil), v.lines...)
	v.mu.Unlock()
	return material.List(v.theme, &v.output).Layout(gtx, len(lines), func(gtx C, index int) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		label := material.Body1(v.theme, lines[index].text)
		label.Color = lines[index].color
		label.TextSize = unit.Sp(15)
		return layout.UniformInset(unit.Dp(2)).Layout(gtx, label.Layout)
	})
}

var window *app.Window

func main() {
	window = new(app.Window)
	window.Option(app.Title("iSH Go"))
	go func() {
		v := newTerminalView()
		var ops op.Ops
		for {
			e := window.Event()
			switch e := e.(type) {
			case app.FrameEvent:
				gtx := layout.Context{Now: e.Now, Metric: e.Metric, Ops: &ops, Source: e.Source}
				v.Layout(gtx)
				e.Frame(gtx.Ops)
			case app.DestroyEvent:
				if v.guestCancel != nil {
					v.guestCancel()
				}
				os.Exit(0)
			}
		}
	}()
	app.Main()
}
