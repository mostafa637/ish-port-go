set pagination off
set confirm off
set breakpoint pending on
break exit_handler
commands
  silent
  printf "[original guest exit] code=%d eip=0x%x esp=0x%x eax=0x%x ebx=0x%x ecx=0x%x edx=0x%x eflags=0x%x\n", code, task->cpu.eip, task->cpu.esp, task->cpu.eax, task->cpu.ebx, task->cpu.ecx, task->cpu.edx, task->cpu.eflags
  bt 8
  continue
end
run -r /home/ubuntu/ish-port-go/testdata/alpine-x86 /bin/busybox true
info program
quit
