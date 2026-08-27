# Continuation log

## الإضافات في هذه الجولة الحالية

أضيفت طبقة `i386.AddressSpace` مع خرائط bounded، واختبارات للتداخل والعثور على holes و`mprotect` و`munmap` ونسخ مساحة العملية. رُبطت هذه الطبقة بـ `mmap2` و`munmap` و`mprotect` في Linux i386 ABI.

أضيف مسار `execve` يقرأ path وargv وenvp من guest memory، ويستبدل ELF/CPU/AddressSpace داخل العملية مع الحفاظ على VFS وPTY وجدول الملفات. أضيف initial stack يحوي `argc/argv/envp/AT_NULL`.

أضيف Scheduler تعاوني round-robin يدعم fork بنسخ مستقلة من CPU وAddressSpace، وwait4 مع status encoding وreaping. كما أضيفت `LoadWithRuntime` لربط Process بــ VFS وPTY الموجودين في Session Gio.

دُعمت segment overrides `FS/GS` في CPU مع تصفير حالة segment لكل تعليمة، وأصبح `set_thread_area` يقرأ `user_desc.base_addr` ويضبط `GSBase`، كما يحفظ `set_tid_address` قيمة `clear_child_tid` في Kernel. أضيفت اختبارات مستقلة لهذه المسارات وللاتجاه الصحيح في 2B/3B SUB/CMP.

توسّع ELF loader ليحمّل `PT_INTERP` وmusl interpreter، ويبني auxv يتحقق من `AT_PHDR/AT_PHENT/AT_PHNUM/AT_BASE/AT_ENTRY/AT_RANDOM/AT_EXECFN`، ويترك relocations للـ dynamic linker داخل guest بدل تطبيقها مسبقًا على interpreter. كما أضيفت اختبارات RELR direct/bitmap واختبار Alpine auxv. main ET_DYN ما زال عند load bias صفري داخل flat guest map، وهو قيد موثق لا دعم PIE عام.

## إصلاح divergence داخل musl

أظهر التتبع أن مقارنة أسماء الرموز كانت تستخدم opcode `0x3A` مع قراءة operand ذاكرة بحجم 32-bit بدل byte؛ صُحح ذلك وأضيف اختبار regression. أضيفت كذلك `MOV r8,imm8`، و`MOV Sreg,r/m16`، و`XCHG`، و`SHRD`، و`CLD/STD`، و`REP MOVSB/MOVSD/STOSD`، و`MOV moffs`، و`OR/AND/XOR EAX,imm32`، و`SBB`، و`TEST` ذات operand-size 16-bit، و`CMPXCHG`، و`BSF/BSR`، وADC/SBB في مجموعات `0x81/0x83`. صُححت دلالة `BSR` لتبحث من bit 31 نزولًا، وأضيفت اختبارات table-driven لحالات القيم الصفرية وغير الصفرية.

كان musl يستخدم `writev(146)` لإخراج رسالة relocation، فتم تنفيذ syscall مع iovec آمن واختباره. الرسالة كشفت أن المشكلة اللاحقة ليست relocation مزدوجًا بل مسار heap/ABI آخر. كما صُححت `mmap2` لتمرير flags واحترام `MAP_FIXED` واعتبار العنوان غير الثابت hint، مع حماية overflow في `AddressSpace.MapAnonymous`.

## التحقق الحالي

نجحت الأوامر التالية في Linux بعد آخر التعديلات:

```text
gofmt -w cmd internal
go test -race ./...
go vet ./...
go build -trimpath -o ./ishgo ./cmd/ishgo
go build -trimpath -o ./ishrun ./cmd/ishrun
./ishrun -root testdata/alpine-x86 -steps 1000000 testdata/alpine-x86/bin/busybox true
```

خرج `busybox true` بالرمز 0، ثم أثبت probe مستقل أن `busybox echo hello` يخرج بالرمز 0 ويكتب `hello\\n` إلى PTY بعد 187,666 instruction. ساعد إصلاح `BSR` في إنهاء مسار allocator الذي كان يعيد طلبات `brk` بصورة متكررة. هذه نتيجة لأمرين محددين وليست تحققًا من BusyBox أو Alpine العام.

## الحدود المتبقية

المشروع ليس بديلًا كاملًا لـ iSH حتى الآن. ما يزال يلزم إكمال load bias غير الصفري وsymbol versioning وTLS المتقدم، ثم ISA الأوسع وFPU/SSE وpaging، COW وpage faults، signals وthreads وclone flags، blocking scheduler، termios و`/dev/pts` الكامل، sockets/epoll/futex، إدارة heap وbrk/mmap الواقعية، shell POSIX الكامل، apk وAlpine integration، واختبار بناء وتوقيع iOS على macOS/Xcode.

## تحديث v5

أثناء تشغيل مصفوفة Alpine ظهرت divergences في `0f ba` و`0f a4` و`0xd0` و`0xfe` وعمليات ADC/OR/AND/XOR byte. أضيفت semantics واختبارات regression لـ BT/BTS/BTR/BTC immediate، SHLD، shifts byte، INC/DEC byte، وADC register/accumulator.

كشفت `ls` أن dispatcher كان يمرر stat64 output pointer من EDX بدل ECX، وأن بنية stat64 كانت تضع `st_mode` في offset غير صحيح. صُحح ذلك وأضيف `fstatat64(300)` و`statx(383)`، ثم عولجت absolute symlinks داخل guest في VFS مع بقاء symlink escape مرفوضًا. بعد ذلك اجتازت أوامر محددة إضافية، منها `date`, `head`, `wc`, `cut`, `basename`, `dirname`, `test`, `id`, `readlink`, و`busybox sh -c 'echo hello'`.

ما يزال `printf` يخرج بالرمز 1 في probe الحالي، و`tr` يحتاج إدخال PTY تفاعليًا؛ لذلك لا تُعرض هذه الجولة كتوافق shell أو BusyBox عام.

## المصادر المستخدمة

- musl `ldso/dlstart.c`: https://git.musl-libc.org/cgit/musl/tree/ldso/dlstart.c
- musl `ldso/dynlink.c`، الإصدار v1.2.5: https://git.musl-libc.org/cgit/musl/plain/ldso/dynlink.c?h=v1.2.5
- Gio iOS installation: https://gioui.org/doc/install/ios
- iSH source repository: https://github.com/ish-app/ish

## متابعة تنفيذية لاحقة — allocator وx87 وABI الذاكرة

أثبت التتبع أن فشل musl السابق لم يكن سببه mmap وحده. التعليمة `66 25 00 f0` في mallocng كانت تُنفذ كـ AND على EAX مع immediate بحجم 32-bit، فتقرأ bytes التعليمات التالية بدل immediate بحجم 16-bit؛ أدى ذلك إلى إسقاط bit `maplen` من metadata ثم وصول `free_group` إلى HLT guard. صُححت دلالات accumulator operand-size 16، وأضيفت اختبارات لـ `66 89` و`66 8b` و`66 c1` و`66 25`. بعد الإصلاح أصبح `whoami` و`id` و`head` و`wc` و`cat` و`ls` و`readlink` تخرج بنجاح، بعد أن كانت بعض نجاحات الإصدار الخامس false-success بسبب معاملة HLT كخروج ناجح.

توسع CPU Pure Go أيضًا بـ subset x87 مبرهن من مسار BusyBox/musl، شمل FLDZ وFLD1 وFLD/FST/FSTP للـ32/64-bit، FILD/FISTP للـ32/64-bit، m80 extended load/store، FADD/FMUL/FSUBP/FMULP، FPREM وFTST وFNSTSW وSAHF وFXCH وFSQRT وFABS وcontrol word. اجتاز `sleep 0` و`sleep 0.5`، مع بقاء هذا subset غير مكافئ بعد لكل x87 أو SSE.

في Linux i386 ABI أضيف getgroups32/setgroups32، poll مع readiness للـPTY وregular files وHUP/error الأساسي، وصُحح nanosleep ليقرأ `struct timespec` كاملًا ويستجيب للإلغاء بـ EINTR. أضيفت اختبارات poll وnanosleep. كما أصبح mmap2 مع MAP_FIXED ذريًا بالنسبة إلى metadata وbytes عند فشل fd أو الكتابة، وأضيف mremap أولي للتمديد in-place والتقليص والنقل مع MAYMOVE، مع رفض العناوين غير page-aligned في munmap/mprotect واختبارات rollback وmremap.

التحقق الحالي: `go test -race ./...` و`go vet ./...` ناجحان، و`busybox_matrix.sh` يثبت نجاح applets غير الحاجبة التالية على Alpine x86: true، echo، test، cat، head، wc، whoami، id، pwd، ls، readlink، basename، dirname، env، cut، sort، printf، date، uname، stat، sleep 0، sleep 0.5. الجسر التفاعلي `busybox sh -i` ينفذ `echo` و`exit` ويستفيد من poll، لكنه لا يمثل بعد جلسة Gio دائمة ولا يوفر كل Linux signals/threads/futex أو MMU/page permissions حقيقية؛ لذلك لا يُعلن المشروع بديل iSH كاملًا.

## دمج جلسة guest في Session وGio

أضيفت إلى `internal/runtime.Session` واجهتا `StartGuest` و`GuestInput`. الأولى تحل guest path داخل VFS، تنشئ BusyBox process وScheduler في goroutine، وتغلق PTY عند النهاية؛ والثانية ترسل bytes إلى PTY النشط. أضيف اختبار يرسل `echo session-ok` و`exit` إلى `busybox sh -i` ويجمع output حتى يرى النص المتوقع.

أضيف إلى Gio وضع اختياري عبر `ISHGO_GUEST=1` و`ISHGO_ROOT=./testdata/alpine-x86`. في هذا الوضع تُشغّل الواجهة `/bin/busybox sh -i`، وتقرأ output من PTY إلى قائمة العرض، وترسل أسطر الإدخال إلى guest. الوضع الافتراضي بقي RootShell المدمج، لأن جلسة guest الحالية لا تزال تفتقر إلى signal delivery الكامل وthreads وMMU/page permissions والشبكات، ولا يصح تقديمها كتطبيق iSH مكتمل.

## جولة BusyBox الموسعة وABI الملفات

كشفت المصفوفة أن `uniq` كان يعلق لا بسبب CPU، بل لأن kernel بعد `close(0)` كان يعيد fallback إلى PTY، ثم يفتح الملف عند fd3 بدل إعادة استخدام fd0؛ أضيفت حالة `closedFDs` وأولوية fd table في `handleRead` وallocator يعيد أقل descriptor متاح. أصبح `uniq /etc/passwd` ناجحًا.

أضيف `readv` رقم 145، فنجح `od`. وأضيف `statfs64` رقم 268، وvirtual `/proc/meminfo` و`/proc/mounts` مع تتبع symlink للـVFS، فنجحت `free` و`df /etc` و`find /etc -maxdepth 1`. كما صُحح `readlink` ليعيد `EINVAL` عند قراءة directory، فنجح `readlink -f /bin/sh`.

آخر مصفوفة قابلة للتكرار شملت 34 حالة BusyBox، من ELF/dynamic loader والملفات والنصوص إلى x87/time وproc/statfs وfind/readv/hash، وكلها خرجت بـ`rc=0` في هذا rootfs المحدد. هذا لا يثبت توافق BusyBox كاملًا أو Linux ABI كاملًا؛ لا تزال الشبكات، signals delivery، threads، page permissions/MMU، وواجهات kernel الأوسع غير مكتملة.

## حزم milestone v6

أُنشئت حزمة source منفصلة في `/home/ubuntu/ish-port-go-v6-source.tar.gz`، وتستبعد `.git` و`testdata/alpine-x86` المستخرج وملفات `tmp_*.go` وrootfs المضغوط. الحجم وSHA-256 موثقان في ملفي الحزمة الخارجيين `/home/ubuntu/ish-port-go-v6-source.tar.gz.sha256` و`stat` وقت التسليم.

أُنشئت حزمة rootfs منفصلة في `/home/ubuntu/ish-port-go-v6-rootfs.tar.gz` بحجم 3,557,539 بايت، وSHA-256 هو `e1945f6e4192da590eb99f541270396efd430042a50a30b39070ea2d30c86248`. تم فحص قائمة source archive والتأكد من عدم وجود rootfs مستخرج أو probes مؤقتة.


## متابعة بعد حزمة v6

أضيفت نقطة `Kernel.TakePendingSignal` واختبارا mask/action، وربطت بـ`Process.Step`: الإشارة pending غير المحجوبة ذات default disposition تنهي guest بالرمز `128+signal`، والإشارات الافتراضية المتجاهلة المحددة تُمسح، أما custom handler فينتج fault تشخيصيًا لأن signal frame و`rt_sigreturn` الحقيقيين لم يُنفذا بعد. أضيف regression تكاملي يرسل SIGTERM إلى حلقة guest ويتحقق من الخروج 143 قبل تنفيذ instruction.

أضيفت regressions صريحة لـ`close(0)` ثم `open` الذي يعيد descriptor 0، وقراءة file من fd 0 دون السقوط إلى PTY fallback، و`dup2` إلى descriptor مغلق. نجحت `go test ./...` و`go test -race ./...` و`go vet ./...` بعد هذه الجولة. يلزم إعادة إنشاء source archive بعد هذه التغييرات؛ لا يُستخدم archive السابق بوصفه current.


## Signal frame وrt_sigreturn — الجولة التالية

بعد baseline ناجح، أضيف `internal/kernel/signal.go` كمسار Pure Go محدود لـnon-SA_SIGINFO: يحفظ سجلات i386 وEFLAGS وFS/GS وsubset FPU وsignal mask في frame محاذٍ على guest stack، يبدأ handler بعنوان guest مع restorer، ثم يستعيد الحالة عبر `rt_sigreturn` بعد التحقق من magic/version وbounds الأساسية. رُبط syscall 173 بالdispatcher، وربط `Process.Step` تسليم custom handlers بهذا المسار؛ SA_SIGINFO وframe variants المتقدمة ما زالت ترفض صراحة.

أضيفت اختبارات kernel لحفظ واستعادة CPU/FPU/segment/mask واختبارات Process تنفذ handler `RET` ثم restorer `int 0x80` وتتحقق من العودة إلى EIP/ESP الأصليين. بعد ذلك نجحت `go test ./...` و`go test -race ./...` و`go vet ./...`، كما نجحت المصفوفة المصححة التي تبني runner عند غيابه وتثبت 34/34 حالة BusyBox محددة بـrc=0.


## Scheduler-aware FUTEX_WAIT — الجولة التالية

أضيف إلى Kernel مسار اختياري يفعّله `NewScheduler`: عند `FUTEX_WAIT` لا ينتظر syscall على host goroutine، بل يسجل waiter وdeadline ويترك `Process` في حالة `Blocked`. يتفقد Scheduler wake أو timeout في دوراته، ويعيد قيمة syscall عند الاستئناف، بينما يستمر `Scheduler.Run` في الاستجابة لإلغاء context إذا بقيت العملية منتظرة. لم تُفعّل shared-address-space threads؛ `CLONE_VM/CLONE_THREAD` ما زالت مرفوضة.

أضيفت اختبارات Kernel وScheduler تثبت عدم حجب المضيف، الاستئناف بعد `FUTEX_WAKE`، واحترام `context.DeadlineExceeded`. بعد الجولة نجحت `go test ./...` و`go test -race ./...` و`go vet ./...`، ومصفوفة BusyBox المصححة أثبتت 34/34 حالة محددة. يجب إعادة إنشاء source archive بعد هذا السجل الأخير.


## GDB parity comparison

تم تثبيت GDB داخل sandbox وبُني CLI iSH الأصلي من `/home/ubuntu/ish-port` عبر Meson/Ninja بعد تهيئة `deps/linux`. أُنشئت scripts قابلة لإعادة التشغيل: `gdb-original.cmd` توقف عند `exit_handler` وتطبع guest CPU fields، و`gdb-go.cmd` توقف عند `Kernel.Handle` وتطبع guest CPU fields الممررة إلى syscall dispatcher.

باستخدام نفس Alpine x86 rootfs: في `busybox true` خرج المرجع وGo طبيعيًا، ووصل كلا المسارين إلى `exit_group` برقم syscall `0xfc`، مع guest exit code صفر. وفي `busybox echo gdb-parity` طبع كلاهما النص نفسه وخرج طبيعيًا. التقرير التفصيلي هو `gdb-comparison-v3.md`. هذا يثبت behavior parity محدودًا عند حالات مختارة، ولا يثبت تطابق كل instruction أو كل Linux ABI.

خلال المقارنة كشف GDB أيضًا أن ishrun non-interactive كان لا يضخ PTY output إلى stdout؛ أُصلح ذلك وأعيد اختبار echo بنجاح. كما كُشف أثر تفعيل page permissions على fixtures الاختبارات، فأضيفت mappings صريحة لعناوين test buffers بدل تجاوز الحماية.


## v7 package and GDB deliverables

تتضمن جولة v7 page-protection enforcement المرتبط بـ`AddressSpace`، و`Fetch8` للـexecute permission، وإصلاح stdout في ishrun الذي كشفته مقارنة GDB. بُني iSH الأصلي CLI وishrun Go مع debug symbols، وشُغّل GDB على الاثنين في `busybox true` و`busybox echo gdb-parity`؛ التقرير `gdb-comparison-v3.md` يثبت guest exit/normal exit وstdout parity المحدود، ولا يدعي instruction equivalence كاملًا.

نجحت بعد ذلك `go test ./...` و`go test -race ./...` و`go vet ./...` ومصفوفة BusyBox 34/34، مع عدم وجود `tmp_*.go`. حزمة v7 النهائية هي source منفصل عن rootfs: source SHA-256 سيُثبت في الملف الخارجي `ish-port-go-v7-source.tar.gz.sha256`، وrootfs SHA-256 `e1945f6e4192da590eb99f541270396efd430042a50a30b39070ea2d30c86248`. يجب عدم اعتبار archive v6 current بعد هذه الجولة.
