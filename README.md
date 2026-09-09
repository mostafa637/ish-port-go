# iSH Go

إعادة بناء شيفرتها التطبيقية بـ **Go + Gio**، مع runtime طرفي مدمج يعمل دون `cgo` أو `os/exec` في منطق guest. ملفات `cmd/` و`internal/` مكتوبة بـ Pure Go؛ أما Gio نفسه فيستخدم طبقة المنصة اللازمة للرسم والنوافذ، ولذلك لا يعني ذلك أن حزمة Gio الرسومية يمكن اختبارها على Linux مع `CGO_ENABLED=0`. المشروع يفصل واجهة Gio عن runtime، ويضم الآن نواة i386 تفسيرية وطبقات ELF/VFS/PTY/syscall قابلة للتوسعة.

## الحالة الحالية

هذه النسخة runtime Pure Go قادر على تشغيل static ELF32، وتحميل ELF32 ديناميكي يحتوي `PT_INTERP` وتهيئة musl interpreter وauxv داخل guest، مع VFS وPTY وScheduler وواجهة Gio. أثبتت مصفوفة Alpine x86 محددة نجاح applets تشمل `true`, `echo`, `test`, `cat`, `head`, `wc`, `whoami`, `id`, `pwd`, `ls`, `readlink`, `basename`, `dirname`, `env`, `cut`, `sort`, `printf`, `date`, `uname`, `stat`, و`sleep 0.5`. كما تنفذ جلسة `busybox sh -i` المحددة أوامر `echo` و`exit` عبر PTY وpoll. لكنها ليست بديلًا كاملًا لـ iSH؛ هذه نتائج حالات محددة ولا تثبت توافق BusyBox أو Alpine العام. منطق runtime والمحاكي هنا Pure Go، بينما Gio/Xcode مخصصان للرسم والتغليف على المنصة.

النسخة الحالية توفر أوامر Pure Go مدمجة: `help`, `echo`, `pwd`, `cd`, `ls`, `cat`, `env`, `uname`, `whoami`, `date`, `history`, `clear`, `true`, `false`, و`exit`. كما أن واجهة `terminal.Backend` تفصل runtime عن الواجهة، بحيث يمكن استبدال `terminal.Shell` لاحقًا بمنفذ iSH كامل.

| الطبقة | التنفيذ الحالي | قابلية التوسعة |
|---|---|---|
| UI | Gio immediate-mode، شاشة داكنة، سجل قابل للتمرير، إدخال وزران | إضافة لوحة مفاتيح، تبويبات، وإيماءات |
| Runtime | RootShell داخل VFS، Session، static ELF runner، وPT_INTERP/musl مع اختبارات `true` و`echo` و`sh -c` محددة ناجحة | dynamic linker متعدد المكتبات بالكامل وshell POSIX |
| Filesystem | VFS مع rootfs و`/proc` و`/dev` وPTY devices | permissions/inodes/`/dev/pts` الكامل |
| Process model | AddressSpace، execve، fork/wait Scheduler تعاوني، clone fork-style، futex وsignal state، signal frame غير-SA_SIGINFO مع `rt_sigreturn`، وblocked-state لـFUTEX_WAIT داخل Scheduler | CLONE_VM/CLONE_THREAD، SA_SIGINFO والإشارات المتقدمة، thread scheduler الكامل، COW الحقيقي |
| iOS integration | gogio يولد حزمة iOS من Go | يتطلب Xcode للتوقيع والنشر |

## التشغيل على Linux أو macOS

يتطلب المشروع Go 1.24 أو أحدث لأن إصدار Gio المستخدم هو `v0.10.2`.

```sh
go mod tidy
go test -race ./...
go test ./internal/terminal
# اختبار منطق Pure Go وحده دون cgo
CGO_ENABLED=0 go test ./internal/terminal
go run ./cmd/ishgo
```

في بيئة Linux قد تحتاج Gio إلى مكتبات التطوير الخاصة بـ Wayland وX11 وEGL وVulkan، بحسب backend الرسوميات المتاح.

للتجربة الاختيارية لجلسة BusyBox الضيف داخل Gio، شغّل الواجهة مع rootfs Alpine x86 المحلي:

```sh
ISHGO_ROOT=./testdata/alpine-x86 ISHGO_GUEST=1 go run ./cmd/ishgo
```

عند تفعيل `ISHGO_GUEST=1` تُرسل أسطر الإدخال إلى `/bin/busybox sh -i` عبر PTY، وتُقرأ مخرجات الضيف asynchronously إلى قائمة Gio. الوضع الافتراضي لا يزال `RootShell` المدمج حتى لا تُعرض جلسة الضيف التجريبية على أنها shell iSH مكتمل؛ `ishrun -interactive` هو مسار الاختبار الأكثر مباشرة للـPTY.

## البناء لـ iOS

يتطلب بناء Apple وجود Xcode على جهاز macOS. بعد تثبيت أداة Gio:

```sh
go install gioui.org/cmd/gogio@latest
gogio -target ios -appid com.example.ishgo ./cmd/ishgo
```

للمحاكي:

```sh
gogio -o ishgo.app -target ios ./cmd/ishgo
xcrun simctl install booted ishgo.app
```

لإنتاج framework يمكن دمجه في مشروع Xcode:

```sh
gogio -target ios -buildmode archive ./cmd/ishgo
```

هذه الأوامر مبنية على تعليمات Gio الرسمية لبناء تطبيقات iOS.[1]

## بنية الكود

```text
cmd/ishgo/main.go                    واجهة Gio وحلقة أحداث النافذة
cmd/ishrun/main.go                   مشغّل ELF32 للاختبار من سطر الأوامر
internal/i386/                       CPU interpreter وguest memory
internal/elf32/                      ELF32 loader وتهيئة stack
internal/guest/                      Process lifecycle وinstruction budget
internal/kernel/                     Linux i386 syscall ABI وfile descriptors
internal/vfs/                        rootfs آمن وproc/dev virtual mounts
internal/pty/                        canonical input وecho وoutput queues
internal/rootfs/                     استيراد tar.gz مع path traversal protection
internal/runtime/                    Session وRootShell وواجهة runtime لـ Gio
ARCHITECTURE.md                      تقسيم الطبقات وقرارات التصميم
full-port-research.md                قيود iOS ومراجع المعمارية
```

## ما تم تنفيذه وما لم يكتمل

تم تنفيذ واختبار: interpreter i386 جزئي مع ModRM/SIB وsegment overrides، byte/word ALU وstring instructions وBT/SHLD وCMPXCHG وBSF/BSR وADC/SBB وINC/DEC، subset x87 مبرهن من مسار musl، guest memory bounded، AddressSpace وخرائط `mmap2` مع flags وatomic MAP_FIXED rollback و`munmap`/`mprotect`/`mremap`، ELF32 loader مع argv/envp و`PT_INTERP`، auxv أساسي، REL/RELR relocation helpers، `set_thread_area` وGSBase، `writev`، `execve` process replacement، Scheduler تعاوني مع `fork` و`wait4` وclone fork-style، stat64/statx وgetdents64، getgroups وpoll وtimespec nanosleep وFUTEX_WAIT/WAKE وحالة signal mask/action، signal frame غير-SA_SIGINFO مع `rt_sigreturn` واختبار handler/restorer، handlers لملفات ووقت وعشوائية وPTY، VFS مع proc/dev وguest symlinks، rootfs tar.gz importer، وRootShell داخل Session Gio. مصفوفة Alpine المحددة موثقة في `VALIDATION-v6.md`، والاختبارات العامة تشمل `go test -race ./...` و`go vet ./...` وبناء `cmd/ishgo` و`cmd/ishrun` بنجاح في Linux.

لا تزال إعادة التنفيذ **غير متوافقة بالكامل مع iSH**. النواقص الجوهرية المتبقية هي تغطية ISA الكاملة بما فيها SSE، paging وpage faults وCOW، SA_SIGINFO وsignal frame variants المتقدمة، CLONE_VM/CLONE_THREAD وblocking scheduler، ioctl/termios الكامل، sockets/epoll والشبكات، futex المتكامل مع threads، وdynamic linker عام متعدد المكتبات. loader الحالي يحمّل musl interpreter وBusyBox ويترك relocations للـ dynamic linker داخل guest، لكن load bias غير الصفري وsymbol versioning العام غير مكتملين. كما أن Gio ما يزال يستخدم RootShell المدمج ولم تُدمج جلسة guest دائمة داخل الواجهة. يلزم أيضًا shell POSIX كامل، apk وAlpine integration، واختبار build/signing على iOS. لا يصح توزيع النسخة الحالية على أنها بديل كامل لـ iSH قبل إغلاق هذه البنود.

## خطة إكمال التوافق مع iSH

الخطوة التالية هي توسيع scheduler-aware futex إلى wait queues متعددة مرتبطة بعملية/خيط، ثم تنفيذ CLONE_VM/CLONE_THREAD وSA_SIGINFO وsignal frame variants، وبعد ذلك تقوية paging وCOW وpage permissions وload-bias غير الصفري وsymbol versioning وTLS المتقدمة. بالتوازي تُوسّع ISA وLinux ABI بناءً على اختبارات صغيرة قابلة للعزل، ثم يُختبر Alpine rootfs مع `busybox` و`apk` وshell scripts. بعد ذلك تُدمج جلسة guest دائمة في Gio، وأخيرًا تُجرى عملية بناء وتوقيع iOS على macOS/Xcode؛ بيئة Linux الحالية لا تستطيع إثبات حزمة iOS النهائية.

لن تستخدم هذه المراحل C أو Objective-C في منطق التطبيق. ومع ذلك، يظل Xcode جزءًا من سلسلة بناء iOS التي توفرها Gio، لأن منصة Apple تحتاج إلى توقيع وتغليف التطبيق، كما أن Gio يربط نافذة التطبيق وطبقة الرسوميات بخدمات المنصة؛ هذا لا يغيّر كون منطق المشروع وruntime المكتوبين هنا Pure Go.

## الترخيص والمصدر الأصلي

يجب مراجعة `LICENSE.md` و`LICENSE.IOS` في مستودع iSH الأصلي قبل توزيع أي كود مشتق من النواة أو المحاكي. هذا المشروع الحالي يعيد تنفيذ طبقة مستقلة ولا ينسخ ملفات C أو assembly من iSH.

## المراجع

[1]: https://gioui.org/doc/install/ios "Gio: iOS and tvOS installation"
[2]: https://github.com/ish-app/ish "iSH source repository"


## مقارنة المرجع الأصلي عبر GDB

بُني CLI المرجع من `/home/ubuntu/ish-port` عبر Meson/Ninja، وشُغّل GDB على executable الأصلي وعلى `ishrun` Go المبني مع debug symbols، باستخدام نفس Alpine x86 rootfs. في سيناريو `busybox true` وصل المرجع إلى `exit_handler` مع guest `code=0` و`eax=0xfc`، بينما التقط Go سلسلة syscalls وانتهت بـ`eax=0xfc` أيضًا؛ وكلا الـinferiors انتهى طبيعيًا. وفي `busybox echo gdb-parity` طبع كلاهما النص نفسه وانتهى طبيعيًا.

التقرير القابل للمراجعة هو `gdb-comparison-v3.md`، وأوامر GDB هي `gdb-original.cmd` و`gdb-go.cmd`. هذه مقارنة behavior/guest-state عند حدود مختلفة، وليست ادعاء تطابق instruction-by-instruction: GDB يرى guest CPU مباشرة داخل struct المرجع الأصلي، ويرى guest CPU الممرر إلى syscall boundary في Go، أما سجلات host x86-64 فلا تُقارن كسجلات guest i386.


## الجولة الحالية: pipes وshell pipelines

أضيف دعم Pure Go لـ`pipe(2)` رقم 42 و`pipe2(2)` رقم 331 مع endpoints داخلية مشتركة، buffer محدود، EOF بعد إغلاق جميع writers، `EPIPE` عند غياب readers، و`O_NONBLOCK` مع `EAGAIN`. تدعم descriptors الجديدة `dup2` وfork inheritance عبر reference counts مستقلة، ويعرض `poll` readiness وHUP الأساسيين.

عند تشغيل Scheduler، لا يحجب read/write على pipe goroutine المضيف؛ تُحفظ العملية في `Process.Blocked` وتُستأنف عند توفر البيانات أو المساحة. كما أصبح `wait4` blocking scheduler-aware، وأصبح `ishrun` يستخدم Scheduler في الوضعين interactive وnon-interactive. أثبت اختبار تكامل `busybox sh -c 'echo pipe-ok | wc -c'` خروجًا بالرمز 0 ونتيجة `8`، وأضيفت الحالة إلى matrix التشغيلية التي أصبحت 35 حالة محددة.

هذا لا يثبت shell POSIX كاملًا. ما زال `SIGPIPE` كتسليم signal تلقائي، blocking `poll` على pipe، تطبيق `O_CLOEXEC` أثناء `execve`، وpipe wait queues متعددة العمليات خارج التغطية الكاملة، كما أن shared threads و`CLONE_VM/CLONE_THREAD` غير مفعّلة.


## الجولة الحالية: SIGPIPE وclose-on-exec

أضيفت معالجة `SIGPIPE` عند فشل الكتابة إلى pipe بلا readers: تعيد syscall قيمة `-EPIPE` وتضع signal 13 في pending signal state، ثم يمر التسليم عبر نفس boundary الموجود في `Process.Step` وdefault disposition. كما أضيفت دلالات `FD_CLOEXEC` لـ`pipe2(O_CLOEXEC)` و`fcntl(F_GETFD/F_SETFD/F_DUPFD_CLOEXEC)` و`dup3(O_CLOEXEC)`، مع مسح العلم عند `dup2` وإغلاق descriptors المعلّمة فقط بعد نجاح `execve`.

اختبارات kernel تثبت EPIPE وSIGPIPE، flags القراءة، نسخ descriptors، ومسح CLOEXEC عند exec boundary. هذا لا يعني بعد تنفيذ signal delivery الكامل لكل signals؛ ما زالت signal actions المتقدمة و`SA_SIGINFO` وSIGPIPE الخاصة بالـthreads خارج النطاق.
