# iSH Go + Gio — Release Notes v5

## ملخص

هذه نسخة تطويرية قابلة للبناء من إعادة تنفيذ iSH باستخدام **Pure Go + Gio**. توسعت هذه الجولة من تشغيل applets بسيطة إلى مسارات ملفات ودلائل وshell محددة داخل Alpine x86، لكنها لا تزال ليست بديلًا مكتملًا أو متوافقًا بالكامل مع iSH.

## التغييرات الرئيسية

أضيفت تعليمات i386 المطلوبة من اختبارات BusyBox الواقعية: `BT/BTS/BTR/BTC` ذات immediate عبر `0F BA`، و`SHLD` عبر `0F A4`، وshifts byte بمعامل واحد عبر `D0`، و`INC/DEC` byte عبر `FE`، وصيغ ADC للـ byte/dword والـ accumulator، إضافة إلى صيغ OR وAND وXOR byte. أضيفت اختبارات regression مستقلة لهذه المجموعات، مع اختبارات BSR/BSF وCDQ وSTOSB السابقة.

صُحح dispatcher الخاص بـ `stat64` لتمرير output buffer من سجل ECX وفق ABI i386، وصُحح offset `st_mode` في بنية stat. أضيف `fstatat64(300)` و`statx(383)` بالجزء المطلوب لمسارات BusyBox، مع اختبارات AT_FDCWD وlayout الأساسي. كما عولجت absolute symlinks داخل guest rootfs في VFS، مع إبقاء symlink escape إلى خارج rootfs مرفوضًا واختباره.

## نتائج Alpine المثبتة

مصدر الاختبار هو Alpine minirootfs 3.24.0 x86، SHA256:

```text
e1945f6e4192da590eb99f541270396efd430042a50a30b39070ea2d30c86248
```

اجتازت الأوامر التالية end-to-end مع خروج 0 في probes مستقلة أو smoke tests بميزانية خمسة ملايين instruction: `busybox true`، `busybox echo hello`، `busybox sh -c 'echo hello'`، `uname -a`، `pwd`، `date -u`، `cat /etc/os-release`، `head -n 1 /etc/os-release`، `wc -l /etc/os-release`، `cut`، `basename`، `dirname`، `test -f /bin/busybox`، `id`، و`readlink /bin/sh`.

الـ probe المستقل لـ `busybox sh -c 'echo hello'` سجل `exit=0` و`steps=207956` ومخرجات PTY هي `hello\n`. كما سجل `busybox echo hello` سابقًا `exit=0` و`steps=187666` ومخرجات `hello\n`.

أصبح `ls /bin` يمر عبر `getdents64` و`statx` دون رسائل `Function not implemented` أو fault في smoke test، لكن مخرجات directory listing النهائية تحتاج اختبارًا منفصلًا أكثر صرامة قبل اعتبارها توافقًا كاملاً. `printf` ما يزال يخرج بالرمز 1 في probe الحالي، و`tr` التفاعلي ينتظر إدخالًا من PTY ولا ينبغي تشغيله في probe غير تفاعلي.

## التحقق البرمجي

نجحت الأوامر التالية من جذر المشروع:

```text
gofmt -w cmd internal
go test -race ./...
go vet ./...
go build -trimpath -o ./ishgo ./cmd/ishgo
go build -trimpath -o ./ishrun ./cmd/ishrun
```

تشمل الاختبارات الجديدة CPU وkernel وVFS، وتتحقق من stat64/fstatat64/statx، absolute guest symlinks، ADC، BT immediate، SHLD، D0 shifts، FE INC/DEC، CDQ، STOSB، وعمليات المسار التي سببت divergences في الجولة.

## الحدود

ما تزال إعادة التنفيذ **غير متوافقة بالكامل مع iSH**. النواقص الجوهرية تشمل PIE load bias غير الصفري، symbol versioning، TLS descriptors، FPU/SSE، paging وpage faults وCOW، signals وthreads وclone flags، blocking scheduler، futex وsockets وepoll، ioctl/termios و`/dev/pts` الكامل، dynamic linker متعدد المكتبات، shell POSIX الكامل، apk، واختبار build/signing على iOS عبر macOS/Xcode. كما أن إدارة `brk` و`mmap2` ما تزال metadata فوق guest memory متصلة وليست MMU/page allocator حقيقية.

## المراجع

[1]: https://github.com/ish-app/ish "iSH source repository"
[2]: https://gioui.org/doc/install/ios "Gio iOS installation"
[3]: https://git.musl-libc.org/cgit/musl/tree/ldso/dlstart.c "musl dynamic loader startup"
[4]: https://git.musl-libc.org/cgit/musl/plain/ldso/dynlink.c?h=v1.2.5 "musl dynlink.c v1.2.5"
