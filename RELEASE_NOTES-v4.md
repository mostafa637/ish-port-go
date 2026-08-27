# iSH Go + Gio — Release Notes v4

## ملخص

هذه نسخة تطويرية قابلة للبناء من إعادة تنفيذ iSH باستخدام **Pure Go + Gio**. تتضمن runtime i386 تفسيريًا، ELF32/VFS/PTY/syscall layers، Scheduler تعاونيًا، وواجهة Gio. لا تُعد هذه النسخة بديلًا مكتملًا أو متوافقًا بالكامل مع iSH.

## الإنجاز الأهم

نجح الاختبار الحقيقي التالي داخل Alpine x86 minirootfs 3.24.0:

```text
./ishrun -root testdata/alpine-x86 -steps 1000000 testdata/alpine-x86/bin/busybox true
```

خرج الأمر بالرمز **0** بعد إصلاح divergence داخل musl dynamic startup. هذا يثبت تشغيلًا end-to-end لهذا الأمر المحدد، ولا يثبت توافق BusyBox أو Alpine العام.

## التغييرات

صُححت مقارنة byte في opcode `0x3A`، إذ كان interpreter يقرأ operand الذاكرة كقيمة 32-bit بدل byte. أضيفت تعليمات `MOV r8,imm8`، و`MOV Sreg,r/m16` مع FS/GS selectors، و`XCHG`، و`SHRD`، و`CLD/STD`، و`REP MOVSB/MOVSD/STOSD`، و`MOV moffs`، و`OR/AND/XOR EAX,imm32`، و`SBB`، وoperand-size 16-bit لبعض مجموعات ALU وTEST، و`CMPXCHG`، و`BSF/BSR`، وADC/SBB في مجموعة `0x81/0x83`.

أضيف syscall `writev(146)` مع قراءة iovec من guest memory، وهو ما مكّن من ظهور رسالة musl الحقيقية أثناء التشخيص بدل خروج 127 صامتًا. كما صُححت `mmap2` لتستقبل flags وفق ABI i386، وتعتبر العنوان hint عندما لا يكون `MAP_FIXED`، وترفض الخرائط الثابتة خارج guest memory بدل قبول pointer غير صالح. أضيفت حماية overflow إلى `AddressSpace.MapAnonymous`.

أضيفت اختبارات مستقلة لـ CMP byte، وREP MOVSD، وCMPXCHG، وBSF/BSR، وwritev، وmmap2 flags/range، مع إبقاء اختبارات TLS/FS/GS وset_thread_area وset_tid_address وauxv وRELR.

## التحقق

نجحت الأوامر التالية في Linux:

```text
gofmt -w cmd internal
go test -race ./...
go vet ./...
go build -trimpath -o ./ishgo ./cmd/ishgo
go build -trimpath -o ./ishrun ./cmd/ishrun
./ishrun -root testdata/alpine-x86 -steps 1000000 testdata/alpine-x86/bin/busybox true
```

مرجع الاختبار هو Alpine x86 minirootfs 3.24.0، SHA256:

```text
e1945f6e4192da590eb99f541270396efd430042a50a30b39070ea2d30c86248
```

## اختبار echo

نجح probe مستقل لـ `busybox echo hello` بخروج 0 بعد 187,666 instruction، وكانت مخرجات PTY هي `hello\\n`. عالج إصلاح BSR مسارًا في allocator كان يؤدي إلى طلبات `brk` متكررة. هذا النجاح محصور في الأمر المحدد، ولا يمثل shell Alpine أو BusyBox العام.

## الحدود

ما يزال load bias غير الصفري للـ PIE محدودًا، كما أن symbol versioning وTLS descriptors وFPU/SSE وpaging وpage faults وCOW وsignals وthreads وclone flags وblocking scheduler وfutex وsockets وepoll وtermios و`/dev/pts` الكامل وshell POSIX وapk وAlpine integration غير مكتملة. بناء وتوقيع حزمة iOS يحتاج macOS/Xcode، ولم يُثبت من بيئة Linux الحالية.

## المصادر

[1]: https://github.com/ish-app/ish "iSH source repository"
[2]: https://gioui.org/doc/install/ios "Gio iOS installation"
[3]: https://git.musl-libc.org/cgit/musl/tree/ldso/dlstart.c "musl dynamic loader startup"
[4]: https://git.musl-libc.org/cgit/musl/plain/ldso/dynlink.c?h=v1.2.5 "musl dynlink.c v1.2.5"
