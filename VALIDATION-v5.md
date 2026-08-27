# Validation record — v5

تاريخ التحقق: 2026-08-26.

## Build and static checks

نجحت الأوامر التالية من جذر المشروع:

```text
gofmt -w cmd internal
go test -race ./...
go vet ./...
go build -trimpath -o ./ishgo ./cmd/ishgo
go build -trimpath -o ./ishrun ./cmd/ishrun
```

## Alpine x86

مصدر rootfs هو Alpine minirootfs 3.24.0 x86، SHA256:

```text
e1945f6e4192da590eb99f541270396efd430042a50a30b39070ea2d30c86248
```

Smoke tests بميزانية خمسة ملايين instruction خرجت بالرمز 0 للأوامر التالية: `busybox true`، `busybox echo hello`، `busybox uname -a`، `busybox ls /bin`، `busybox date -u`، و`busybox sh -c 'echo hello'`.

## Probes with PTY output

سجل probe `busybox echo hello` خروجًا 0 بعد 187,666 instruction ومخرج `hello\n`.

سجل probe `busybox sh -c 'echo hello'` خروجًا 0 بعد 207,956 instruction ومخرج `hello\n`.

اختبارات applets إضافية خرجت 0: `pwd`، `cat /etc/os-release`، `head -n 1 /etc/os-release`، `wc -l /etc/os-release`، `basename`، `dirname`، `test -f /bin/busybox`، `id`، `readlink /bin/sh`، `grep`، `cut`، `sort`، و`sed -n 1p /etc/os-release` بعد إصلاحات الجولة.

## إصلاحات مثبتة بالاختبارات

تتضمن الجولة semantics واختبارات لـ `0F BA` BT/BTS/BTR/BTC، و`0F A4` SHLD، وD0 byte shifts، وFE byte INC/DEC، وADC byte/dword/accumulator، وOR/AND/XOR byte، وCDQ، إلى جانب stat64/fstatat64/statx، absolute guest symlinks، getdents64، وبنية i386 stat.

## حدود الأدلة

`busybox printf` ما يزال يخرج بالرمز 1 دون output في probe الحالي، و`tr` التفاعلي ينتظر stdin ولذلك لا يمكن تقييمه عبر probe غير تفاعلي لا يحقن input. نجاح applets المحددة لا يثبت shell POSIX أو BusyBox العام أو Alpine العام.

ما تزال PIE load bias غير الصفري، FPU/SSE، paging/page faults/COW، signals/threads/futex/sockets، dynamic linker متعدد المكتبات، termios و`/dev/pts` الكامل، وإثبات build/signing لـ iOS غير مكتملة.
