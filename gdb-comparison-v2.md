# GDB comparison v2

## Scenario
Same Alpine x86 rootfs and BusyBox `true`; original uses `-r` realfs, Go uses `-root`.

## Original iSH GDB wrapper rc: 0
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
[original guest exit] code=0 eip=0xf7f54362 esp=0xffffdd68 eax=0xfc ebx=0x0 ecx=0x0 edx=0xf7ffaf9c eflags=0x44
#0  exit_handler (task=0x5555558ff440, code=0) at ../xX_main_Xx.h:19
#1  0x00005555555665aa in do_exit (status=0) at ../kernel/exit.c:114
#2  0x0000555555566797 in do_exit_group (status=0) at ../kernel/exit.c:146
#3  0x00005555555669cc in sys_exit_group (status=0) at ../kernel/exit.c:191
#4  0x0000555555585bd3 in handle_interrupt (interrupt=128) at ../kernel/calls.c:271
#5  0x0000555555561d87 in task_run_current () at ../kernel/task.c:106
#6  0x000055555556047b in main (argc=5, argv=0x7fffffffe168) at ../main.c:20
[Inferior 1 (process 164276) exited normally]
The program being debugged is not being run.

## Go GDB wrapper rc: 0
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
Breakpoint 1 at 0x56a056: file /home/ubuntu/ish-port-go/internal/kernel/syscall.go, line 265.
[New LWP 164291]
[New LWP 164292]
[New LWP 164293]
[New LWP 164294]
[New LWP 164295]
[Switching to LWP 164294]
[go guest syscall] eip=0x100673e8 esp=0x108a7d98 eax=0xf3 ebx=0x108a7d98 ecx=0x100a5290 edx=0xffffffff eflags=0x46
[go guest syscall] eip=0x10040020 esp=0x108a7dc0 eax=0x102 ebx=0x100a6cfc ecx=0x100a5290 edx=0x100a6c64 eflags=0x46
[go guest syscall] eip=0x100411e2 esp=0x108a7c8c eax=0x2d ebx=0x0 ecx=0x0 edx=0x100a4f9c eflags=0x46
[go guest syscall] eip=0x100411e2 esp=0x108a7c8c eax=0x2d ebx=0xcb000 ecx=0x0 edx=0x100a4f9c eflags=0x6
[go guest syscall] eip=0x100411e2 esp=0x108a7c34 eax=0xc0 ebx=0xc9000 ecx=0x1000 edx=0x0 eflags=0x46
[go guest syscall] eip=0x10032926 esp=0x108a7c60 eax=0x7d ebx=0x100a4000 ecx=0x1000 edx=0x1 eflags=0x6
[Switching to LWP 164292]
[go guest syscall] eip=0x10032926 esp=0x108a7c60 eax=0x7d ebx=0xc6000 ecx=0x2000 edx=0x1 eflags=0x6
[Switching to LWP 164294]
[go guest syscall] eip=0x100411e2 esp=0x108a7e98 eax=0xc7 ebx=0xc79d0 ecx=0x2f edx=0xa72c5 eflags=0x2
[go guest syscall] eip=0x100411e2 esp=0x108a7dc8 eax=0xfc ebx=0x0 ecx=0x0 edx=0x100a4f9c eflags=0x6
[LWP 164295 exited]
[LWP 164294 exited]
[LWP 164293 exited]
[LWP 164292 exited]
[LWP 164291 exited]
[Inferior 1 (process 164288) exited normally]
The program being debugged is not being run.

## Interpretation
- Original GDB stopped in `exit_handler` with guest exit code 0 and showed the original emulator CPU state.
- Go GDB stopped in the Go kernel syscall boundary and printed the guest CPU state passed to the syscall handler.
- Host x86-64 registers are not treated as guest i386 registers; guest fields above are the comparison data.
