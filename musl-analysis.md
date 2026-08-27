# Musl dynamic-startup analysis

مصدر musl الرسمي يوضح أن `ldso/dlstart.c` يقرأ `argc/argv/auxv`، ويستخدم `AT_BASE` لحساب load base للـ interpreter، ثم يطبق فقط relocations relative المبكرة على interpreter قبل استدعاء `__dls2`. كما أن `dynlink.c` يعرّف `__dls2b` كمرحلة إعداد مبكر لـ TLS، ويستخدم `find_sym` وGNU/SysV hash lookup.

في التتبع المحلي، نداء 0x10033fca إلى وظيفة lookup عند 0x10031920 يعود بـ EAX=0 في الحالة الأخيرة، ثم يقرأ caller القيمة `[0+4] = 0x10101` ويضيف `base=0x10000000`، فينشأ target `0x10010101` داخل `.dynstr`. هذا يعني أن المشكلة ليست في FF call نفسه؛ بل في أن lookup أعاد NULL أو أن state/arguments/hash/flags داخل lookup انحرفت.

تصحيح parity flags في ADD/SUB لم يغيّر failure. current loader يحمّل main ET_DYN عند bias صفر وinterpreter عند 0x10000000، ويترك relocations للـ in-guest musl، مع auxv مصحح واختبارات له.

## External sources

- musl `ldso/dlstart.c`: https://git.musl-libc.org/cgit/musl/tree/ldso/dlstart.c
- musl `ldso/dynlink.c` at v1.2.5: https://git.musl-libc.org/cgit/musl/plain/ldso/dynlink.c?h=v1.2.5

المصدر v1.2.5 يوضح أن `do_relr_relocs` يتخطى `ldso` في `reloc_all` لأن self-relocation أُنجز في `_dlstart`، وأن symbolic relocations للـ ldso تُطبق في stage 2. كما يوضح أن `ldso_fail` يؤدي إلى `_exit(127)` بعد محاولة relocation النهائية. التتبع المحلي أظهر أن 127 كان سببه أولًا `writev` غير المنفذ، وبعد تنفيذ writev ظهرت الرسالة الدقيقة: `Error relocating /bin/busybox: __register_frame_info_bases: symbol not found`.
