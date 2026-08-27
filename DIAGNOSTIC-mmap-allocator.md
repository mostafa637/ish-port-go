# تشخيص mmap وmusl allocator — 2026-08-26

أعيد اختبار BusyBox بعد إصلاح MOV operand-size 16. `go test -race ./...` نجح، لكن `whoami`, `head`, `wc`, و`id` ما زالت تصل إلى HLT musl عند `0x10045b75`، لذلك لا تعد ناجحة.

## trace mmap2 في `whoami`

1. `step=14278`: `mmap2(addr=0x000c9000, len=0x1000, prot=0, flags=0x32, fd=-1, pgoff=0)` أعاد `0x000c9000`. هذا يتطابق مع guard page بعد مسار `brk(0)` في mallocng: `MAP_ANON|MAP_PRIVATE|MAP_FIXED|PROT_NONE`.
2. `step=185958`: `mmap2(addr=0, len=0x1000, prot=3, flags=0x22, fd=-1, pgoff=0)` أعاد `0x01000000`. هذا mapping anonymous private writable.
3. قبل الإصلاح كان trace التالي يصل إلى `lea edi,[eax-0x10]` مع `eax=0x01000000` ثم يقرأ zero من `0x00fffff0` ويضرب HLT. بعد إصلاح `66 89/8B` السلوك النهائي لم يتغير.

## مقارنة musl mallocng

مصدر musl الرسمي يعرّف `UNIT=16`, و`enframe()` يعيد pointer المستخدم `p` داخل `g->mem->storage + stride*idx`، وليس mmap base مباشرة. في مسار mmap الفردي، `m->mem = p` حيث `p` هو mmap base، ثم `enframe(g, idx, n, ctr)` يحسب `p = g->mem->storage + stride*idx`; وبما أن `struct group` header يسبق `storage`, فإن pointer المستخدم يبدأ عادة بعد 16-byte header، ويجب أن تكون bytes في `p-16..p-1` قد كُتبت بواسطة `enframe` (`p[-4]`, `p[-3]`, `p[-2]`, size metadata).

المصدر الرسمي: https://git.musl-libc.org/cgit/musl/plain/src/malloc/mallocng/malloc.c وmeta.h.

## فرضية العمل الحالية

MOV operand16 كان فعلاً ناقصًا: `66 89/8B` كانا يستخدمان writeRM/readRM 32-bit. تم إصلاح ذلك وإضافة `TestMovOperand16RegisterAndMemoryForms`، لكن allocator HLT ما زال قائمًا. يلزم الآن trace instruction-level بعد mmap، خصوصًا قيمة pointer المرجعة من `enframe`، وقراءة/كتابة bytes عند `0x00ffffe0..0x01000040`. يجب التحقق من صيغ CPU الأخرى التي تستخدم metadata: `66 89`, `66 8B`, `66 8D`, `66 C7`, byte stores، والعمليات التي تحسب `p-16`.

## قيود mmap الحالية

`mmap2` يستخدم `nextMap=0x02000000`، ثم يعيد أول mapping عند `0x01000000` بسبب allocator العنوان الافتراضي الحالي. MAP_FIXED يزيل mapping قبل نجاح map ولا يملك rollback عند الفشل. الذاكرة flat bounded slice وليست MMU/page-fault حقيقية. لا يجوز حل HLT بكتابة fake header أو اعتباره نجاحًا.

## مصادر musl الرسمية المستخدمة للمقارنة

- `mallocng/malloc.c`: https://git.musl-libc.org/cgit/musl/plain/src/malloc/mallocng/malloc.c
- `mallocng/meta.h`: https://git.musl-libc.org/cgit/musl/plain/src/malloc/mallocng/meta.h
- `mallocng/realloc.c`: https://git.musl-libc.org/cgit/musl/plain/src/malloc/mallocng/realloc.c
- `mallocng/free.c`: https://git.musl-libc.org/cgit/musl/plain/src/malloc/mallocng/free.c

تؤكد هذه المصادر أن `UNIT=16`، وأن `get_meta(p)` يفترض pointer مستخدمًا محاذيًا مع header عند `p-16`، بينما mapping الفردي يخزن `struct group` عند base ويعيد `enframe(...)` pointer بعد header. كما تؤكد أن `free` على `g->mem` لا يصح إلا في مسار free_group الداخلي بعد التحقق، لا كـ user pointer عام.
