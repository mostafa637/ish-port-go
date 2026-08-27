set pagination off
set confirm off
set breakpoint pending on
break 'example.com/ish-go/internal/kernel.(*Kernel).Handle'
commands
  silent
  printf "[go guest syscall] eip=0x%x esp=0x%x eax=0x%x ebx=0x%x ecx=0x%x edx=0x%x eflags=0x%x\n", cpu.EIP, cpu.Regs[4], cpu.Regs[0], cpu.Regs[3], cpu.Regs[1], cpu.Regs[2], cpu.EFLAGS
  continue
end
run -root /home/ubuntu/ish-port-go/testdata/alpine-x86 -steps 18000000 /home/ubuntu/ish-port-go/testdata/alpine-x86/bin/busybox true
info program
quit
