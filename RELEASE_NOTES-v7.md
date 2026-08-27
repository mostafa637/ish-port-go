# Release Notes v6 — iSH Port Go

**الحالة:** milestone قابل لإعادة البناء والاختبار، وليس إصدار iSH كاملًا.

## الملخص

توسّع هذا milestone من runtime i386 تجريبي إلى بيئة ضيف عملية قادرة على تحميل ELF32 ديناميكي مع `PT_INTERP` وmusl، تشغيل BusyBox Alpine x86، استخدام VFS محصور داخل rootfs، تمرير الملفات والـPTY، وتنفيذ shell تفاعلي عبر قناة guest اختيارية. جميع منطق المحاكي والـruntime مكتوب بـPure Go؛ يستخدم Gio للرسم والتغليف فقط، ولا يعتمد على `os/exec` لتشغيل البرامج الضيفة.

التحقق الحالي يستخدم rootfs Alpine x86 المحلي المستخرج من `alpine-minirootfs-3.24.0-x86.tar.gz`. نجاح عملية ما يعني أنها نفذت `exit` syscall فعلية ولم تصل إلى HLT guard في musl؛ وقد أزيلت سياسة HLT القديمة التي كانت تخفي أخطاء allocator على أنها خروج ناجح.

## ما أضيف أو صُحح

| المجال | الحالة المثبتة في v6 |
|---|---|
| CPU i386 | MOV/ALU وoperand-size 16، shifts 16-bit، BT cross-word، SBB، BSWAP، وsubset x87 المطلوب لمسار BusyBox الزمني |
| ELF وmusl | ELF32/PT_INTERP، stack/auxv، musl self-relocation داخل guest، وBusyBox PIE |
| الملفات وVFS | root confinement، symlinks guest، virtual `/proc` و`/dev`، `readv`، `statfs64`، `sysinfo`، وإعادة استخدام fd منخفض بعد close |
| الذاكرة | `brk` وmetadata للـheap، anonymous/file-backed `mmap2`، MAP_FIXED rollback ذري، page checks، و`mremap` للتمديد/التقليص/النقل مع MAYMOVE |
| PTY وshell | canonical input، echo، half-close، cancellation، `poll` readiness، و`nanosleep` بــtimespec صحيح |
| العمليات والتزامن | fork-style `clone`، `wait`، FUTEX_WAIT/WAKE مع timeout، scheduler-aware `Process.Blocked` لـFUTEX_WAIT، signal mask/action/pending state، non-SA_SIGINFO signal frame/handler/restorer و`rt_sigreturn`، وkill validation؛ لا توجد SA_SIGINFO أو threads مشتركة |
| Gio | guest mode اختياري عبر `ISHGO_GUEST=1` و`ISHGO_ROOT`، مع Session lifecycle وPTY output/input asynchronous |

## مصفوفة BusyBox المحددة

شغّلت `scripts/busybox_matrix.sh` على `testdata/alpine-x86` بعد آخر التعديلات. الحالات التالية خرجت بـ`rc=0`: `true`، `echo`، `test`، `cat`، `head`، `wc`، `whoami`، `id`، `pwd`، `ls`، `readlink`، `basename`، `dirname`، `env`، `cut`، `sort`، `printf`، `date`، `uname`، `stat`، `sleep 0`، `sleep 0.5`، `grep`، `uniq`، `md5sum`، `sha256sum`، `od`، `find`، `df`، `du`، `ps`، `free`، `which`، و`readlink -f`.

كما يثبت اختبار runtime إرسال `echo session-ok` و`exit` إلى `busybox sh -i` عبر `Session.StartGuest` و`GuestInput`، وتثبت اختبارات kernel وProcess حفظ واستعادة CPU/FPU/segment/mask عبر handler/restorer و`rt_sigreturn`. نجحت `go test -race ./...` و`go vet ./...` بعد الجولة الأخيرة، كما شغلت المصفوفة المصححة 34 حالة مع build تلقائي وفشل حقيقي عند أي rc غير صفري.

## القيود المعروفة

هذا milestone لا يقدّم توافق Linux كاملًا ولا يقدّم بديلًا كاملًا لـiSH. نموذج scheduler-aware الحالي يوقف task واحدًا عند FUTEX_WAIT دون حجب المضيف، ويستأنفه عند wake/timeout أو ينهي Run عند إلغاء context؛ لا يمثل بعد wait queues متعددة أو threads مشتركة. نموذج الذاكرة ما زال flat bounded guest memory مع mapping metadata وليس MMU/page fault/COW حقيقيًا، كما أن صلاحيات الصفحات ليست مفروضة على كل قراءة وتنفيذ CPU. إشارات Linux مخزنة وقابلة للاختبار جزئيًا لكن SA_SIGINFO وdelivery variants المتقدمة لم تُستكمل؛ المسار المثبت حاليًا هو non-SA_SIGINFO frame محدود مع restorer و`rt_sigreturn`. `clone` يرفض shared-address-space threads بدل إنشاء threads وهمية، والشبكات وsockets وpoll على كل أنواع الملفات خارج نطاق التحقق الحالي. كما أن subset x87 ليس FPU كاملًا، ولا يوجد دعم عام لكل SSE أو privileged i386 instructions.

وضع Gio guest mode تجريبي اختياري، بينما يبقى RootShell المدمج هو الافتراضي. لا توجد في هذا المستودع حزمة iOS signed أو مشروع Xcode نهائي؛ بناء Apple يحتاج بيئة macOS/Xcode خارج sandbox، وتبقى طبقة الرسم والتغليف منفصلة عن منطق المحاكي.

## إعادة البناء والتحقق

```sh
cd /home/ubuntu/ish-port-go
gofmt -w $(find cmd internal scripts -name '*.go' -type f)
go test ./...
go test -race ./...
go vet ./...
go build -trimpath -o ./bin/ishrun ./cmd/ishrun

# runner مباشر
./bin/ishrun -root ./testdata/alpine-x86 ./testdata/alpine-x86/bin/busybox whoami

# guest PTY تفاعلي
printf 'echo from-pty\nexit\n' | ./bin/ishrun -interactive -root ./testdata/alpine-x86 ./testdata/alpine-x86/bin/busybox sh -i

# Gio guest mode الاختياري
ISHGO_ROOT=./testdata/alpine-x86 ISHGO_GUEST=1 go run ./cmd/ishgo
```

لا يضم source archive rootfs المستخرج أو ملفات `tmp_*.go`. يبقى rootfs المضغوط منفصلًا مع ملف SHA-256 الخاص به.

## المراجع

[1]: https://git.musl-libc.org/cgit/musl/tree/src/malloc/mallocng/malloc.c "musl mallocng source"

[2]: https://man7.org/linux/man-pages/man2/mmap2.2.html "Linux mmap2(2)"

[3]: https://man7.org/linux/man-pages/man2/readv.2.html "Linux readv(2)"


## v7: page protection وGDB parity

أضيفت طبقة `AddressSpace` اختيارية لفرض صلاحيات `ProtRead` و`ProtWrite` و`ProtExec` على وصول guest، مع `Fetch8` للـinstruction fetch و`ReadRaw/WriteRaw` لتهيئة loader فقط. يفعّل ELF loader الحماية بعد تحميل segments وstack، ويؤدي الوصول إلى صفحة غير mapped أو بصلاحية خاطئة إلى fault واضح. أضيفت regressions مباشرة لـread/write/execute و`mprotect` وcross-page access.

أُصلح مسار ishrun non-interactive ليضخ PTY output إلى stdout بعد أن كشف GDB اختلافًا عن iSH الأصلي. بُني CLI المرجع الأصلي وishrun Go مع debug symbols، وشُغّل GDB على الاثنين باستخدام نفس Alpine x86 rootfs. أثبت `busybox true` exit behavior وguest exit syscall، وأثبت `busybox echo gdb-parity` تطابق stdout والـnormal exit. التقرير التفصيلي والـGDB command files موجودة في المصدر.

لا يعني ذلك تطابقًا instruction-by-instruction؛ فبنية C interpreter وGo interpreter مختلفة، والمرجع يستخدم realfs بينما Go يستخدم VFS محصورًا، كما أن MMU/page faults/COW وshared threads وLinux ABI الكامل ما زالت خارج نطاق هذا milestone.
