# مواصفات إعادة التنفيذ الكاملة

## المعمارية المرجعية

يوضح توثيق iSH أن النظام يتكون من أربعة أجزاء مترابطة: محاكي x86/x86_64، طبقة kernel لترجمة Linux syscalls إلى Darwin/iOS، نظام ملفات افتراضي، وطبقة TTY/PTY. تدفق الأمر يبدأ من إدخال المستخدم، ثم تنفيذ التعليمات، ثم اعتراض syscall، وترجمته، والوصول إلى filesystem، وإرجاع النتيجة إلى الذاكرة المحاكية.

المحاكي المرجعي يستخدم threaded-code interpreter وMMU/TLB، بينما تعتمد النواة على حالة CPU وسجل syscall. نظام الملفات هجين: metadata افتراضية مع passthrough للبيانات، إضافة إلى /proc و/dev و/dev/pts.

## قيود iOS

توثيق Apple يصف App Sandbox بأنه يحد وصول التطبيق إلى موارد النظام وبيانات المستخدم من خلال entitlements. لذلك يجب أن يكون rootfs داخل container التطبيق أو ضمن ملفات اختارها المستخدم، ولا يجوز افتراض وجود Linux kernel أو إمكانية fork/exec لبرامج خارجية. التشغيل الصحيح على iOS هو user-mode execution داخل عملية التطبيق، مع runtime مكتوب بـ Go.

## قرار التنفيذ

ستُكتب طبقات CPU والذاكرة وELF وsyscalls وfilesystem وPTY بـ Pure Go. واجهة التطبيق ستكون Gio، لكن بناء Gio على iOS يظل يحتاج إلى Xcode وطبقة native packaging التي توفرها gogio. لا ينبغي استخدام JIT أو executable memory؛ سيتم اعتماد interpreter أو threaded dispatch آمن قابل للتنفيذ داخل ذاكرة التطبيق.

## المصادر

1. https://phineas1500-ish.mintlify.app/architecture/overview
2. https://github.com/ish-app/ish
3. https://developer.apple.com/documentation/security/app-sandbox
4. https://gioui.org/doc/install/ios

## محاكيات Pure Go المقارنة

يوجد مشروع ThreeAteSix مكتوب بـ Go ويذكر دعم real/protected mode وsegmentation/paging وinterrupts وتعليمات 80386، لكنه مشروع PC emulator مستقل وليس user-mode Linux syscall emulator، لذلك لا يحل طبقات ELF أو Linux ABI أو VFS الخاصة بـ iSH.

يوجد أيضًا tiny_x86_emu مكتوب بـ Pure Go، لكنه تجريبي وموجه حاليًا لتشغيل xv6 فقط. لا يمكن إدخاله مباشرة كبديل لمحاكي iSH دون مراجعة الترخيص والتوافق وإعادة بناء واجهات الذاكرة والمقاطعات.

المراجع:
5. https://github.com/andrewjc/threeatesix
6. https://github.com/bobuhiro11/tiny_x86_emu

## مراجعة المتابعة — 2026-08-25

الحالة الحالية مبنية وقابلة للاختبار، لكنها لا تزال تستخدم guest memory متصلة واحدة. لذلك فإن `mmap2` يحجز عنوانًا منطقيًا فقط، و`munmap` لا يزيل خريطة، ولا توجد صلاحيات صفحات أو نسخ address space. كما أن `SysExecve` و`SysFork` و`SysWait4` تعيد `ENOSYS` عمدًا. الخطوة التالية الصحيحة هي إضافة `AddressSpace` وخرائط bounded قبل ربط process replacement؛ وإلا فسيكون تنفيذ execve شكليًا وغير آمن.

المراجع الأصلية داخل مستودع iSH التي ينبغي محاكاتها تدريجيًا هي `kernel/exec.c` لمسار استبدال mm، و`kernel/mmap.c` لإدارة الخرائط، و`kernel/fork.c` لنموذج نسخ/مشاركة address space.

## نتائج التنفيذ اللاحقة

أضيفت طبقة `i386.AddressSpace` مع خرائط bounded وعمليات `MapAnonymous`, `Remove`, `Protect`, `Clone`, وربطت بـ `mmap2`, `munmap`, و`mprotect`.

أضيف مسار `execve` يقرأ path وargv وenvp من guest memory، ويستبدل ELF/CPU/AddressSpace داخل العملية مع الحفاظ على VFS وPTY وfile table. أضيف Scheduler تعاوني round-robin يدعم fork بنسخ مستقلة من address space وwait4 مع status/reaping؛ لا يزال ذلك دون COW وsignals وblocking wait الكامل.

توسعت Linux i386 ABI باختبارات للملفات والوقت والعشوائية وdirectory operations وPTY. يرفض loader ملفات `PT_INTERP` برسالة `DynamicLinkerError` typed، لأن تشغيلها دون dynamic linker سيعطي سلوكًا مضللًا.

## Alpine x86 baseline — 2026-08-25

تم تنزيل Alpine minirootfs x86 الرسمي من:
`https://dl-cdn.alpinelinux.org/alpine/latest-stable/releases/x86/alpine-minirootfs-3.24.0-x86.tar.gz`

تم التحقق من checksum: `e1945f6e4192da590eb99f541270396efd430042a50a30b39070ea2d30c86248`.

يحتوي rootfs على `/bin/busybox` كملف ELF32 PIE dynamic interpreter، ويستخدم `/lib/ld-musl-i386.so.1`. musl interpreter نفسه ELF32 static-pie. تحميل PT_INTERP وخرائط interpreter وREL/RELR relocations أصبح يعمل في loader، لكن تشغيل BusyBox توقف بعد عدة آلاف من التعليمات عند opcode/semantics ناقصة، وليس عند تحميل ELF.

نتيجة آخر تشغيل: بعد دعم ALU groups وSIB وbyte operations وshift/FF/prefixes، وصل التنفيذ إلى `unsupported opcode 0x0f` داخل musl قرب offset `0x33ea2`. فحص disassembly يشير إلى أن المنطقة تتضمن TLS/segment-related startup code؛ لذلك يلزم تنفيذ extended `0x0f` instructions وsegment/TLS semantics قبل اعتبار musl startup قابلاً للتشغيل.

## Dynamic loader وTLS — نتائج الجولة الحالية

تمت إضافة دعم `FSBase` و`GSBase` إلى CPU، مع تفسير prefixes `0x64/0x65` لكل تعليمة وتصفير segment state بين التعليمات. يقرأ syscall `set_thread_area` الحقل `base_addr` من `user_desc` ويضبط `GSBase`، بينما يحفظ `set_tid_address` مؤشر `clear_child_tid` داخل Kernel. هذه طبقة ABI أولية وليست بعدُ تنفيذًا كاملًا لـ TLS descriptors أو clone/thread teardown أو futex wakeups.

أثبتت تجارب Alpine أن تطبيق RELR على interpreter قبل بدء musl كان خطأً دلاليًا: القيمة الخام عند relocation target `0xa52b4` هي `0x0006fb0f`، وبعد host pre-relocation تصبح `0x1006fb0f`، ثم يعيد musl self-relocation إضافة load bias فتظهر قيمة خاطئة `0x2006fb0f`. لذلك يترك loader الحالي relocation tables للـ dynamic linker داخل guest، مع بقاء helper واختبارات RELR منفصلة.

تم تصحيح auxv بحيث يكون `AT_BASE` موجودًا فقط مع `PT_INTERP`، ويشير `AT_ENTRY` إلى entry المحمل، كما أضيف اختبار يقرأ stack الفعلي ويتحقق من `AT_PHDR`, `AT_PHENT`, `AT_PHNUM`, `AT_BASE`, `AT_ENTRY`, `AT_RANDOM`, و`AT_EXECFN`. ما يزال main ET_DYN محمّلًا عند load bias صفري داخل flat guest memory، ولذلك لا ينبغي اعتبار ذلك دعمًا عامًا لتوزيع PIE.

آخر اختبار تشغيل حقيقي:

```text
./ishrun -root testdata/alpine-x86 -steps 1000000 testdata/alpine-x86/bin/busybox true
```

يتقدم الاختبار إلى dynamic startup داخل musl، ثم يتوقف عند `0x10010101`، وهو عنوان داخل `.dynstr` وليس بداية instruction صالحة. التتبع يبين أن divergence يظهر أثناء مسار lookup داخلي مثل `__dls2b` قبل استدعاء pointer غير صالح، ولذلك تبقى الأسباب المحتملة في semantics تعليمات/flags إضافية، stack/calling convention، أو dynamic-linker state. لا يثبت هذا الاختبار تشغيل BusyBox end-to-end.


## نتائج جولة v4

أثبت الاختبار end-to-end تشغيل `busybox true` من Alpine x86 داخل runtime، مع خروج 0 عند حد مليون instruction. تم عزل divergence السابق في سلسلة من فروق ISA والـ flags: CMP byte كان يقرأ 32-bit، كما كانت هناك حاجة إلى MOV r8 immediate، string instructions، 16-bit operand-size، SHRD، SBB/ADC، CMPXCHG، وBSF/BSR.

أظهر syscall tracing أن musl يستخدم `writev(146)` لإخراج أخطاء dynamic linker؛ تنفيذ writev كشف الرسالة السابقة الخاصة بالرمز weak، ثم أدى تصحيح operand-size إلى تجاوزها. أضيفت أيضًا MAP_FIXED/hint semantics إلى mmap2 وحراسة overflow في AddressSpace. بعد إصلاح BSR، نجح probe مستقل لـ `busybox echo hello` بخروج 0 ومخرجات PTY `hello\\n` عند 187,666 instruction. لا ينبغي تعميم نجاح الأمرين المحددين على توافق BusyBox الكامل.

المراجع الخارجية ذات الصلة:

- https://git.musl-libc.org/cgit/musl/tree/ldso/dlstart.c
- https://git.musl-libc.org/cgit/musl/plain/ldso/dynlink.c?h=v1.2.5
- https://github.com/ish-app/ish
- https://gioui.org/doc/install/ios

## نتائج جولة v5

أظهرت مصفوفة Alpine أن الفشل التالي بعد v4 لم يكن في dynamic loader، بل في فروق ISA وLinux ABI أثناء applets الملفات والنصوص. أضيفت BT/BTS/BTR/BTC immediate (`0F BA`)، وSHLD (`0F A4`)، وshifts byte (`D0`)، وINC/DEC byte (`FE`)، وصيغ ADC/OR/AND/XOR byte وADC accumulator. كما عولجت دلالة CDQ واختبرت كل إضافة بعينات CPU صغيرة.

أظهر trace لـ `ls` أن stat64 كان يمرر عنوان output من السجل الخطأ، وأن `st_mode` في بنية stat كان عند offset غير صحيح. بعد التصحيح أضيف `fstatat64(300)` و`statx(383)` بالجزء الذي يتطلبه BusyBox، وأضيفت اختبارات AT_FDCWD وlayout. كما عولجت absolute symlinks مثل `/bin/ls -> /bin/busybox` داخل VFS مع استمرار رفض الروابط التي تخرج من rootfs.

اجتازت probes مستقلة أوامر محددة مثل `date`, `head`, `wc`, `cat`, `cut`, `basename`, `dirname`, `test`, `id`, `readlink`، و`busybox sh -c 'echo hello'`. سجل shell probe خروجًا 0 ومخرجات `hello\\n` بعد 207,956 instruction. تبقى `printf` غير ناجحة في probe الحالي، كما أن `tr` التفاعلي يحتاج input PTY؛ لذلك لا تمثل هذه النتائج توافق BusyBox أو shell POSIX العام.
