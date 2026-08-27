# iSH Go — Release Notes v3

## الحالة

هذه نسخة تطويرية قابلة للبناء من إعادة تنفيذ iSH باستخدام **Pure Go + Gio**. تتضمن runtime i386 تفسيريًا، ELF32/VFS/PTY/syscall layers، Scheduler تعاونيًا، وواجهة Gio. لا تُعد هذه النسخة بديلًا مكتملًا أو متوافقًا end-to-end مع iSH.

## ما أُنجز في هذه النسخة

أضيف دعم `PT_INTERP` لتحميل Alpine musl interpreter وتهيئة initial stack وauxv، مع اختبارات فعلية لحقول `AT_PHDR`, `AT_PHENT`, `AT_PHNUM`, `AT_BASE`, `AT_ENTRY`, `AT_RANDOM`, و`AT_EXECFN`. أضيفت REL/RELR relocation helpers واختبارات direct/bitmap، مع تصحيح مهم: لا تُطبق relocations على interpreter قبل تشغيل dynamic linker حتى لا تُضاف load bias مرتين.

أضيف دعم أولي لـ `FS/GS` segment overrides، وتمثيل `FSBase/GSBase` في CPU، وتنفيذ `set_thread_area` و`set_tid_address` في Linux i386 syscall layer. كما صُححت دلالات اتجاه `SUB/CMP` في opcodes `2B/3B` وأضيفت اختبارات CPU مستقلة.

## التحقق

تم تشغيل الأوامر التالية بنجاح في Linux:

```text
gofmt -w cmd internal
go test -race ./...
go vet ./...
go build -trimpath -o ./ishgo ./cmd/ishgo
go build -trimpath -o ./ishrun ./cmd/ishrun
```

تم استخدام Alpine x86 minirootfs 3.24.0 الرسمي للاختبار، مع checksum:

```text
e1945f6e4192da590eb99f541270396efd430042a50a30b39070ea2d30c86248
```

## الاختبار المعروف الفاشل

الأمر التالي لا ينجح بعد:

```text
./ishrun -root testdata/alpine-x86 -steps 1000000 testdata/alpine-x86/bin/busybox true
```

يتقدم التنفيذ إلى musl dynamic startup ثم يتوقف عند قفزة إلى `0x10010101` داخل `.dynstr`. هذا يعني أن dynamic startup ما زال يحتاج إلى استكمال ISA/flags أو stack/calling convention أو symbol lookup/dynamic-linker semantics. main ET_DYN ما يزال عند load bias صفري داخل flat guest map، ولا يوجد دعم عام لـ nonzero PIE bias أو symbol versioning أو TLS descriptors.

## الحدود الكبرى

لم يكتمل بعد FPU/SSE، paging وpage faults، COW، signals، threads وclone flags، futex وblocking scheduler، termios و`/dev/pts` الكامل، sockets/epoll، shell POSIX، Alpine `apk`، أو بناء وتوقيع iOS عبر Xcode. لذلك يجب استخدام هذه النسخة كقاعدة تطوير واختبار، لا كتطبيق iSH جاهز.
