# GDB comparison v1

## Reference iSH original
Command: `build-gdb/ish -r testdata/alpine-x86 /bin/busybox true`

GDB wrapper rc: 0
warning: File "/home/ubuntu/ish-port/ish-gdb.gdb" auto-loading has been declined by your `auto-load safe-path' set to "$debugdir:$datadir/auto-load".
To enable execution of this file add
	add-auto-load-safe-path /home/ubuntu/ish-port/ish-gdb.gdb
line to your configuration file "/home/ubuntu/.config/gdb/gdbinit".
To completely disable this security protection add
	set auto-load safe-path /
line to your configuration file "/home/ubuntu/.config/gdb/gdbinit".
For more information about this security protection see the
"Auto-loading safe path" section in the GDB manual.  E.g., run from the shell:
	info "(gdb)Auto-loading safe path"
Breakpoint 1 at 0xbd9c: file ../xX_main_Xx.h, line 19.
[Thread debugging using libthread_db enabled]
Using host libthread_db library "/lib/x86_64-linux-gnu/libthread_db.so.1".

Breakpoint 1, exit_handler (task=0x5555558ff440, code=0) at ../xX_main_Xx.h:19
19	    if (task->parent != NULL)
\n[original] stopped at exit_handler\nrax            0x5555558ff440      93824996078656
rbx            0x55555555fd89      93824992279945
rcx            0x3                 3
rdx            0x0                 0
rsi            0x0                 0
rdi            0x5555558ff440      93824996078656
rbp            0x7fffffff9d50      0x7fffffff9d50
rsp            0x7fffffff9d40      0x7fffffff9d40
r8             0x5555558ff010      93824996077584
r9             0x7                 7
r10            0x5555558ff2a0      93824996078240
r11            0x4f315142685052cf  5706431548514915023
r12            0x5                 5
r13            0x0                 0
r14            0x5555556488f8      93824993233144
r15            0x7ffff7ffd000      140737354125312
rip            0x55555555fd9c      0x55555555fd9c <exit_handler+19>
eflags         0x202               [ IF ]
cs             0x33                51
ss             0x2b                43
ds             0x0                 0
es             0x0                 0
fs             0x0                 0
gs             0x0                 0
k0             0xfeffc100          4278173952
k1             0x1                 1
k2             0x1                 1
k3             0x0                 0
k4             0x0                 0
k5             0x0                 0
k6             0x0                 0
k7             0x0                 0
fs_base        0x7ffff7d58800      140737351354368
gs_base        0x0                 0
#0  exit_handler (task=0x5555558ff440, code=0) at ../xX_main_Xx.h:19
#1  0x00005555555665aa in do_exit (status=0) at ../kernel/exit.c:114
#2  0x0000555555566797 in do_exit_group (status=0) at ../kernel/exit.c:146
#3  0x00005555555669cc in sys_exit_group (status=0) at ../kernel/exit.c:191
#4  0x0000555555585bd3 in handle_interrupt (interrupt=128) at ../kernel/calls.c:271
#5  0x0000555555561d87 in task_run_current () at ../kernel/task.c:106
#6  0x000055555556047b in main (argc=5, argv=0x7fffffffe168) at ../main.c:20
[Inferior 1 (process 163081) exited normally]
\n[original] final inferior state\nThe program being debugged is not being run.

## Go implementation
GDB wrapper rc: 0
warning: File "/home/ubuntu/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.24.0.linux-amd64/src/runtime/runtime-gdb.py" auto-loading has been declined by your `auto-load safe-path' set to "$debugdir:$datadir/auto-load".
To enable execution of this file add
	add-auto-load-safe-path /home/ubuntu/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.24.0.linux-amd64/src/runtime/runtime-gdb.py
line to your configuration file "/home/ubuntu/.config/gdb/gdbinit".
To completely disable this security protection add
	set auto-load safe-path /
line to your configuration file "/home/ubuntu/.config/gdb/gdbinit".
For more information about this security protection see the
"Auto-loading safe path" section in the GDB manual.  E.g., run from the shell:
	info "(gdb)Auto-loading safe path"
Breakpoint 1 at 0x57fa56: file /home/ubuntu/ish-port-go/cmd/ishrun/main.go, line 14.
Breakpoint 2 at 0x57dbd6: file /home/ubuntu/ish-port-go/internal/guest/process.go, line 75.
[New LWP 164120]
[New LWP 164121]
[New LWP 164122]
[New LWP 164123]

Thread 1 "ishrun-gdb" hit Breakpoint 1, main.main () at /home/ubuntu/ish-port-go/cmd/ishrun/main.go:14
14	func main() {
\n[go] stopped at first breakpoint\nrax            0x57fa40            5765696
rbx            0xc000022140        824633860416
rcx            0xc000002380        824633729920
rdx            0x5eb1d8            6205912
rsi            0x0                 0
rdi            0x2f208a28          790661672
rbp            0xc00007af68        0xc00007af68
rsp            0xc00007af68        0xc00007af68
r8             0x2d7a18            2980376
r9             0x1                 1
r10            0x7ffff7ff9080      140737354109056
r11            0x133acc            1260236
r12            0xc00007ae08        824634224136
r13            0xffffffffffffffff  -1
r14            0xc000002380        824633729920
r15            0x2031              8241
rip            0x57fa56            0x57fa56 <main.main+22>
eflags         0x202               [ IF ]
cs             0x33                51
ss             0x2b                43
ds             0x0                 0
es             0x0                 0
fs             0x0                 0
gs             0x0                 0
k0             0x0                 0
k1             0x0                 0
k2             0x0                 0
k3             0x0                 0
k4             0x0                 0
k5             0x0                 0
k6             0x0                 0
k7             0x0                 0
fs_base        0x6dfa70            7207536
gs_base        0x0                 0
#0  main.main () at /home/ubuntu/ish-port-go/cmd/ishrun/main.go:14
[Switching to LWP 164121]

Thread 3 "ishrun-gdb" hit Breakpoint 2, example.com/ish-go/internal/guest.(*Process).Step (p=0xc0000121c0, ~r0=...) at /home/ubuntu/ish-port-go/internal/guest/process.go:75
75	func (p *Process) Step() error {
\n[go] stopped at second breakpoint\nrax            0xc0000121c0        824633795008
rbx            0x0                 0
rcx            0x0                 0
rdx            0x0                 0
rsi            0xc000100410        824634770448
rdi            0x112a880           18000000
rbp            0xc00011dc40        0xc00011dc40
rsp            0xc00011dc40        0xc00011dc40
r8             0x5c1220            6033952
r9             0xc000013f80        824633802624
r10            0x0                 0
r11            0x0                 0
r12            0xc00011d8f0        824634890480
r13            0x10000000          268435456
r14            0xc000002380        824633729920
r15            0xb                 11
rip            0x57dbd6            0x57dbd6 <example.com/ish-go/internal/guest.(*Process).Step+22>
eflags         0x206               [ PF IF ]
cs             0x33                51
ss             0x2b                43
ds             0x0                 0
es             0x0                 0
fs             0x0                 0
gs             0x0                 0
k0             0x0                 0
k1             0x0                 0
k2             0x0                 0
k3             0x0                 0
k4             0x0                 0
k5             0x0                 0
k6             0x0                 0
k7             0x0                 0
fs_base        0xc00004c898        824634034328
gs_base        0x0                 0
#0  example.com/ish-go/internal/guest.(*Process).Step (p=0xc0000121c0, ~r0=...) at /home/ubuntu/ish-port-go/internal/guest/process.go:75
#1  0x000000000057ef6e in example.com/ish-go/internal/guest.(*Process).Run (p=0xc0000121c0, ctx=..., maxSteps=18000000, ~r0=...) at /home/ubuntu/ish-port-go/internal/guest/process.go:146
#2  0x000000000057fe8c in main.main () at /home/ubuntu/ish-port-go/cmd/ishrun/main.go:34

Thread 3 "ishrun-gdb" hit Breakpoint 2, example.com/ish-go/internal/guest.(*Process).Step (p=0xc0000121c0, ~r0=...) at /home/ubuntu/ish-port-go/internal/guest/process.go:75
75	func (p *Process) Step() error {
\n[go] final inferior state\nLast stopped for thread 3 (LWP 164121).
	Using the running image of child process 164117.
Program stopped at 0x57dbd6.
It stopped at breakpoint 2.
