# معمارية iSH Go الكاملة

## المبدأ

تُفصل واجهة Gio عن runtime تمامًا. لا تستدعي الواجهة `os/exec` ولا تعتمد على عملية shell خارجية؛ بل تتعامل مع كائن session يقدم stdin/stdout وبيانات الحالة. بهذه الطريقة يعمل runtime نفسه داخل عملية iOS، وهو النموذج المطلوب لتشغيل userland محاكى داخل sandbox.

| الحزمة | المسؤولية | الاعتماد المسموح |
|---|---|---|
| `internal/i386` | سجلات CPU، flags، الذاكرة، decoder/interpreter | Go standard library فقط |
| `internal/elf32` | قراءة ELF32 وتحميل segments وstack | `internal/i386` |
| `internal/kernel` | ABI وsyscall dispatch وtask state | `i386`, `vfs`, `pty` |
| `internal/vfs` | mounts وinodes وpermissions و`/proc` و`/dev` | standard library |
| `internal/pty` | line discipline، echo، canonical mode، queues | standard library |
| `internal/runtime` | shell session وrootfs lifecycle | kernel + elf32 |
| `cmd/ishgo` | Gio window وterminal renderer وinput | runtime + Gio |

## طبقات التنفيذ

يستخدم CPU interpreter دورة fetch/decode/execute ويحتفظ بـ EIP وESP والسجلات العامة وEFLAGS. الذاكرة guest منفصلة عن pointers الخاصة بـ Go، وجميع القراءات والكتابات تمر عبر حدود تحقق لمنع الوصول خارج الصورة المحاكية. لا يستخدم المشروع JIT أو generated executable memory.

يقرأ ELF loader ملفات ELF32 little-endian، يتحقق من `EM_386` و`PT_LOAD`، ينشئ `AddressSpace` bounded بخرائط صلاحيات، ينسخ segments إلى guest memory، ويهيئ stack مع `argc/argv/envp/AT_NULL`. يدعم `execve` استبدال الصورة داخل العملية مع الحفاظ على VFS وPTY والـ file table. يرفض `PT_INTERP` حاليًا برسالة typed واضحة إلى أن يُنفذ dynamic linker بدل تشغيل ملف ديناميكي بصورة غير صحيحة.

تعرّف طبقة kernel syscall table وفق Linux i386 ABI. التنفيذ الحالي يغطي I/O وfile descriptors و`open/openat/read/write/lseek/stat/getdents`، إدارة الخرائط `mmap2/munmap/mprotect/brk`، `execve`، fork/wait hooks، الهوية، الوقت، random، directory operations، وPTY ioctl الأساسي. كل syscall يحوّل pointers الضيف إلى buffers آمنة، ولا يسمح بتسريب host paths خارج rootfs.

يقدم VFS مسارًا افتراضيًا موحدًا. rootfs هو directory-backed storage داخل sandbox، بينما `/proc` و`/dev` special mounts. يضيف rootfs importer استخراج tar.gz مع حد للحجم ومنع traversal، وتستعمل PTY مقابض الأجهزة داخل kernel. ما يزال `/dev/pts` وmetadata/permissions الكاملان ضمن الأعمال المتبقية.

يحتوي `internal/guest/scheduler.go` على Scheduler تعاوني round-robin داخل عملية Go واحدة. ينشئ fork نسخة مستقلة من `AddressSpace` وCPU، ويعيد wait4 حالة الخروج ويكتب status إلى ذاكرة الأب. هذا ليس بعدُ COW أو blocking scheduler كاملًا، لكنه يثبت حدود task lifecycle اللازمة قبل إضافة signals والـ threads.

## سياسة النطاق

الوصول إلى توافق iSH كامل لا يعني نسخ ملفات C وAssembly آليًا. كل مكوّن سيعاد تنفيذه باختبارات مستقلة، ثم تُضاف تعليمات x86 وsyscalls حسب نتائج الاختبارات. أي أمر غير مدعوم يجب أن يعيد `ENOSYS` أو رسالة واضحة، لا أن يتصرف بسلوك غير محدد.
