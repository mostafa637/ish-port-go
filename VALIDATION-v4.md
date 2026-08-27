# Validation record — v4

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

نجحت اختبارات الحزم التالية ضمن `go test -race ./...`: `internal/i386`, `internal/elf32`, `internal/guest`, `internal/kernel`, `internal/pty`, `internal/rootfs`, `internal/runtime`, `internal/terminal`, و`internal/vfs`. أوامر `cmd/ishgo` و`cmd/ishrun` لا تحتوي اختبارات Go مستقلة، لكنها بُنيت بنجاح.

## Alpine x86 runtime checks

مصدر rootfs هو Alpine minirootfs 3.24.0 x86، SHA256:

```text
e1945f6e4192da590eb99f541270396efd430042a50a30b39070ea2d30c86248
```

نجح الأمر:

```text
./ishrun -root testdata/alpine-x86 -steps 1000000 testdata/alpine-x86/bin/busybox true
```

وكان الخروج بالرمز 0.

كما نجح probe مستقل للأمر:

```text
busybox echo hello
```

وكانت النتيجة: `state=Exited`, `exit=0`, `steps=187666`, ومخرجات PTY هي `hello\n`.

## Scope of evidence

تثبت هذه النتائج تحميل ELF32 مع `PT_INTERP`، بدء musl dynamic linker داخل guest، تنفيذ BusyBox لهذين الأمرين المحددين، وبعض مسارات Linux i386 ABI. لا تثبت توافق BusyBox العام، أو shell POSIX، أو apk، أو full Alpine، أو بديلًا مكتملًا لـ iSH/iOS.

القيود الرئيسية موثقة في `README.md` و`RELEASE_NOTES-v4.md`: PIE load bias غير الصفري، FPU/SSE، paging/page faults/COW، signals/threads/futex/sockets، termios و`/dev/pts` الكامل، dynamic linker متعدد المكتبات، وإثبات بناء وتوقيع iOS على macOS/Xcode.
