# VALIDATION v6 — حالة التنفيذ الحالية

## نطاق السجل

هذا السجل يصف ما تم تشغيله وإثباته في runtime i386 Pure Go داخل `/home/ubuntu/ish-port-go` حتى هذه الجولة. لا يعني نجاح الحالات المحددة أن المشروع بديل كامل لـ iSH، ولا يعني أن كل BusyBox أو Alpine يعملان. ما يزال التنفيذ user-mode bounded داخل ذاكرة guest flat، مع metadata للخرائط وليس MMU أو page faults حقيقية.

## فحوص البناء والاختبار

نجحت الأوامر التالية بعد آخر التغييرات:

```text
gofmt -w internal/i386 internal/kernel internal/pty
go test -race ./...
go vet ./...
```

كما تحقق عدم وجود ملفات تشخيص مؤقتة من نمط `tmp_*.go` داخل المشروع بعد انتهاء كل probe. أضيفت اختبارات CPU لـ operand-size 16 وSBB وx87، واختبارات kernel لـ poll وnanosleep وgetgroups وfutex وsignals وMAP_FIXED rollback وmremap وpage alignment.

## مصفوفة BusyBox المحددة

تستخدم الحالات التالية Alpine x86 rootfs المحلي و`busybox` الديناميكي مع musl interpreter. الرمز صفر يعني أن العملية نفذت exit syscall وخرجت دون fault؛ لا تُستنتج منه تغطية عامة.

| الحالة | النتيجة المثبتة |
|---|---:|
| `true` | 0 |
| `echo hello from busybox` | 0 |
| `test -f /etc/os-release` | 0 |
| `cat /etc/os-release` | 0 |
| `head -n 2 /etc/os-release` | 0 |
| `wc -l /etc/os-release` | 0 |
| `whoami` و`id` | 0 |
| `pwd` و`ls /bin` | 0 |
| `readlink /bin/sh` | 0 |
| `basename` و`dirname` | 0 |
| `env` و`cut` و`sort` | 0 |
| `printf` و`date` و`uname -a` و`stat` | 0 |
| `sleep 0` و`sleep 0.5` | 0 |
| `grep`, `uniq`, `md5sum`, `sha256sum`, `od` | 0 |
| `find /etc -maxdepth 1`, `df /etc`, `du`, `ps`, `free` | 0 |
| `which sh` و`readlink -f /bin/sh` | 0 |
| `busybox sh -i` مع `echo` و`exit` عبر PTY وpoll | 0 |

## إصلاحات جوهرية مثبتة

أثبت trace مسار mallocng أن `66 25 00 f0` كانت تُفكك بقراءة immediate بحجم 32-bit بدل 16-bit. أدى ذلك إلى ابتلاع bytes التعليمات التالية وإزالة bit `maplen` من metadata، ثم ظهور HLT guard في musl. بعد إصلاح accumulator operand-size 16 و`66 C1` وMOV 16-bit، عاد pointer allocator إلى مسار `base + UNIT` الصحيح دون إنشاء header وهمي.

أضيف subset x87 المطلوب من مسار sleep والتحويلات الزمنية، ويتضمن العمليات الأساسية على 32-bit و64-bit و80-bit extended، مع status/control word والعمليات الحسابية المستخدمة فعليًا في musl. تم اختبار `sleep 0.5` بنجاح بعد معالجة كل fault متسلسل ظهر من disassembly؛ لا يدعي ذلك دعم كل x87 أو SSE.

أضيفت getgroups32 وreadv وsysinfo وstatfs64 وpoll وtimespec nanosleep، وأصبح poll قادرًا على رؤية newline أو EOF في PTY دون استهلاك الإدخال. أضيفت أيضًا نقطة تسليم pending signals على حدود Process: الإشارات الافتراضية تنهي العملية بالرمز `128+signal`، والإشارات الافتراضية المتجاهلة تُمسح. أضيف signal frame غير-SA_SIGINFO يحفظ CPU/FPU/segment/mask، ويبدأ handler من guest stack، ويستعيد الحالة عبر `rt_sigreturn`; اختبارات kernel وProcess تغطي alignment والاستعادة وhandler/restorer. ما يزال SA_SIGINFO وsignal frame variants المتقدمة غير مدعومة، وأخطاء frame غير الصالح تُظهر fault صريحًا. كما أصبح fd allocator يعيد استخدام descriptors منخفضة بعد close، وأصبح فتح virtual files يتتبع symlink داخل guest namespace. أصبح mmap2 مع MAP_FIXED ذريًا بالنسبة إلى metadata وbytes عند فشل file descriptor، وأضيف mremap للتمديد in-place والتقليص والنقل مع MAYMOVE. أضيف FUTEX_WAIT/WAKE الأساسي، وsignal mask/action state وkill validation. كما أضيف مسار scheduler-aware اختياري يجعل `FUTEX_WAIT` حالة `Process.Blocked` غير حاجبة للمضيف، ويستأنفها عند `FUTEX_WAKE` أو timeout مع احترام إلغاء Scheduler context. أما wait queues المتعددة للthreads، وSA_SIGINFO، وتسليم الإشارات المرتبط بالthreads فما زالت غير منفذة.

## الحدود المعروفة

| المجال | الحالة الحالية |
|---|---|
| CPU | interpreter i386 موسع مع x87 subset؛ ما تزال ISA غير مكتملة، وSSE وامتدادات كثيرة غير مدعومة |
| الذاكرة | flat bounded byte array مع mapping metadata؛ لا paging أو COW أو page permissions مفروضة لكل instruction |
| PIE/ELF | PT_INTERP وmusl وELF32 يعملان في المسارات المثبتة؛ load bias غير صفري وsymbol versioning العام غير مكتملان |
| العمليات | fork/wait4 وexecve وclone fork-style، وProcess.Blocked لـFUTEX_WAIT داخل Scheduler؛ CLONE_VM/CLONE_THREAD مرفوضان صراحة حتى تنفيذ نموذج threads حقيقي |
| الإشارات | default dispositions، وnon-SA_SIGINFO frame/handler/restorer و`rt_sigreturn` مثبتة باختبارات؛ SA_SIGINFO وvariants المتقدمة غير مكتملة |
| التفاعل | CLI PTY وBusyBox shell تفاعلي يعملان في الاختبار المحدد؛ Gio ما يزال يستخدم RootShell المدمج ولم تُدمج جلسة guest دائمة بعد |
| الشبكات وAlpine | sockets وepoll وapk والتكامل العام مع Alpine ليست مثبتة بعد |
| iOS | لا يوجد بعد تطبيق iOS signed أو مسار Xcode نهائي؛ Gio هو طبقة الرسم والتغليف المخطط لها فقط |

## ملاحظة منهجية

تم تصحيح سياسة HLT: HLT دون `exit` أو`exit_group` يُسجل fault بدل اعتباره نجاحًا. لذلك أُزيلت من ادعاءات الإصدار الخامس نتائج applets التي كانت تعتمد على هذا false-success، ولا تُعاد إضافتها إلا بعد خروج guest syscall حقيقي.

## References

[1]: https://github.com/ish-app/ish "iSH source repository"
[2]: https://git.musl-libc.org/cgit/musl/plain/src/malloc/mallocng/malloc.c "musl mallocng source"
[3]: https://git.musl-libc.org/cgit/musl/plain/src/malloc/mallocng/meta.h "musl mallocng metadata definitions"
[4]: https://git.musl-libc.org/cgit/musl/plain/src/malloc/mallocng/free.c "musl mallocng free path"


## مقارنة GDB مع iSH الأصلي

بُني CLI المرجع الأصلي من `/home/ubuntu/ish-port` بنجاح عبر Meson/Ninja بعد تهيئة submodule Linux، وبُني `ishrun` Go مع `-gcflags='all=-N -l'`. شُغّل GDB على الاثنين باستخدام نفس Alpine x86 rootfs. في `busybox true` أظهر المرجع `exit_handler` مع guest `code=0` و`eax=0xfc`، وأظهر Go سلسلة guest syscalls انتهت بـ`eax=0xfc`؛ انتهت الحالتان طبيعيًا. وفي `busybox echo gdb-parity` طبع المرجع وGo النص نفسه وانتهى كلاهما طبيعيًا.

التقرير هو `gdb-comparison-v3.md`، مع scripts قابلة لإعادة التشغيل في `gdb-original.cmd` و`gdb-go.cmd`. هذه المقارنة تثبت نقاطًا سلوكية وسجلات guest عند syscall/exit، لكنها لا تثبت تطابق كل instruction أو كل ABI؛ بنية GDB مختلفة بين threaded C interpreter وGo interpreter، وسجلات host x86-64 ليست guest i386 state.


## تغييرات v7 والتحقق الإضافي

في هذه الجولة أضيفت طبقة حماية اختيارية للذاكرة مرتبطة بـ`AddressSpace`: عمليات guest read/write تتحقق من وجود mapping والصلاحية، وinstruction fetch يتحقق من `ProtExec`. أضيفت `mprotect` regressions، ورفض fetch من mapping غير executable، واختبارات cross-page/unmapped access. يستخدم loader `WriteRaw` أثناء initialization ثم يفعّل الحماية قبل بدء guest execution؛ لا تزال هذه طبقة page permissions فوق byte array وليست MMU أو page faults أو COW حقيقية.

كشف GDB أن ishrun non-interactive كان يراكم output داخل PTY ولا يضخه إلى stdout؛ أُصلح ذلك وأثبت `busybox echo gdb-parity` تطابق النص والـexit behavior مع المرجع الأصلي.

أعيد التحقق بعد v7: `go test ./...` و`go test -race ./...` و`go vet ./...` ومصفوفة BusyBox المصححة 34/34 حالة. تقرير GDB وscripts المقارنة مرفقة داخل المصدر باسم `gdb-comparison-v3.md` و`gdb-original.cmd` و`gdb-go.cmd`.
