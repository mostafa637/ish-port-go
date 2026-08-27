# نتائج الفحص الأولي

## iSH
- المستودع الأصلي هو Linux shell for iOS باستخدام user-mode x86 emulation وsyscall translation.
- اللغات الحالية تقريبًا: C 70.1%، Objective-C 21.3%، Assembly 5.4%، Swift 0.8%، Meson 0.8%، Shell 0.6%.
- النواة والمحاكي موزعان على `kernel/`, `fs/`, `emu/`, `asbestos/`, `vdso/`, و`util/`، بينما واجهة iOS في `app/` وتعتمد على UIKit وObjective-C وStoryboard.
- بناء iSH الأصلي يتطلب Xcode وMeson وNinja وClang/LLD وSQLite وlibarchive، إضافة إلى submodule للنواة.
- نقطة تشغيل سطر الأوامر تستدعي `xX_main_Xx` ثم تهيئ device nodes وprocfs وdevpts وتشغل المهمة الحالية.
- محرك `asbestos` يعتمد على gadgets مكتوبة بـ assembly وtail-call threaded dispatch؛ لذلك نقله إلى Pure Go يتطلب إعادة كتابة المحاكي، وليس مجرد تحويل الواجهة.

## Gio
- Gio إطار واجهة immediate-mode مكتوب بـ Go ويدعم iOS وmacOS وLinux وWindows وAndroid.
- بناء iOS الرسمي يتم عبر `gogio -target ios`، ويتطلب Xcode لمنصات Apple.
- يمكن إنتاج `.app` للمحاكي أو framework للتكامل مع Xcode، مع استخدام `GioAppDelegate` عند التكامل.

## الاستنتاج
النقل الكامل 1:1 من iSH إلى Pure Go + Gio مشروع كبير متعدد المراحل. النسخة العملية الأولى يجب أن تفصل بين: (1) واجهة terminal بـ Gio، (2) PTY/session abstraction، (3) shell/command runtime قابل للاستبدال، (4) منفذ لاحق لمحاكي i386 وطبقة Linux syscalls. لا يجوز الادعاء بأن تشغيل Alpine/i386 على iOS تحقق ما لم تُنفذ الطبقتان الأخيرتان وتُختبرا على جهاز Apple.

## المصادر
1. https://github.com/ish-app/ish
2. https://gioui.org/doc/install/ios
