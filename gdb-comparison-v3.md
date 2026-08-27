# GDB comparison v3

## Matched scenarios
Both executables used the same Alpine x86 rootfs. The original used iSH `-r` realfs; Go used `-root`. GDB was run on both executables.

## `busybox true` guest-state traces
Original GDB wrapper rc: 0
[original guest exit] code=0 eip=0xf7f54362 esp=0xffffdd68 eax=0xfc ebx=0x0 ecx=0x0 edx=0xf7ffaf9c eflags=0x44
[Inferior 1 (process 166375) exited normally]
Go GDB wrapper rc: 0
[go guest syscall] eip=0x100673e8 esp=0x108a7d98 eax=0xf3 ebx=0x108a7d98 ecx=0x100a5290 edx=0xffffffff eflags=0x46
[go guest syscall] eip=0x10040020 esp=0x108a7dc0 eax=0x102 ebx=0x100a6cfc ecx=0x100a5290 edx=0x100a6c64 eflags=0x46
[go guest syscall] eip=0x100411e2 esp=0x108a7c8c eax=0x2d ebx=0x0 ecx=0x0 edx=0x100a4f9c eflags=0x46
[go guest syscall] eip=0x100411e2 esp=0x108a7c8c eax=0x2d ebx=0xcb000 ecx=0x0 edx=0x100a4f9c eflags=0x6
[go guest syscall] eip=0x100411e2 esp=0x108a7c34 eax=0xc0 ebx=0xc9000 ecx=0x1000 edx=0x0 eflags=0x46
[go guest syscall] eip=0x10032926 esp=0x108a7c60 eax=0x7d ebx=0x100a4000 ecx=0x1000 edx=0x1 eflags=0x6
[go guest syscall] eip=0x10032926 esp=0x108a7c60 eax=0x7d ebx=0xc6000 ecx=0x2000 edx=0x1 eflags=0x6
[go guest syscall] eip=0x100411e2 esp=0x108a7e98 eax=0xc7 ebx=0xc79d0 ecx=0x2f edx=0xa72c5 eflags=0x2
[go guest syscall] eip=0x100411e2 esp=0x108a7dc8 eax=0xfc ebx=0x0 ecx=0x0 edx=0x100a4f9c eflags=0x6
[Inferior 1 (process 166387) exited normally]

## `busybox echo gdb-parity` observable comparison
Original GDB wrapper rc: 0
gdb-parity
[Inferior 1 (process 166405) exited normally]
Go GDB wrapper rc: 0
gdb-parity
[Inferior 1 (process 166417) exited normally]

## Interpretation
The original GDB stop is at its exit handler and exposes the original emulator CPU fields.
The Go GDB stop is at the Go kernel syscall boundary and exposes the guest CPU fields passed to the dispatcher.
Host x86-64 registers are not compared as guest registers. The parity criteria here are guest exit syscall, successful inferior exit, and matching echo output.
