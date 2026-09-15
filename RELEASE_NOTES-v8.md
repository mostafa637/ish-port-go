# Release Notes v8 — Shared VM Thread Foundation

This milestone adds the first real shared-address-space path for i386 `clone(2)` in the Pure Go runtime.

## Implemented

The kernel now exposes an `OnClone` callback and recognizes the i386 clone register ABI. A clone using `CLONE_VM|CLONE_THREAD` creates a scheduler task with an independent CPU/register state while sharing the parent `AddressSpace`, file-descriptor table, current directory, futex queues, and signal actions. The optional child stack, TLS/GS base, `CLONE_PARENT_SETTID`, and `CLONE_CHILD_SETTID` arguments are forwarded and tested. Thread exit avoids closing process-wide descriptors.

The existing fork-style path remains copy-on-write-in-spirit but physically cloned, and clone flags that do not include the supported shared-VM/thread pair continue to use the established fork compatibility path or return `ENOSYS` as appropriate.

## Verification

`go test ./...` passes for all packages, including a regression test that validates the i386 clone argument registers and the new callback path. Formatting and `git diff --check` also pass.

## Deliberate limitations

This is a foundation, not full Linux thread compatibility. `getpid`/thread-group identity, `exit_group`, robust futex lists, per-thread signal delivery, `CLONE_CHILD_CLEARTID` wakeups, shared mmap metadata synchronization, concurrent host execution, and complete TLS semantics still require implementation. The scheduler remains cooperative and does not claim general POSIX thread support. iOS build/signing and Alpine `apk` validation are also not established by this milestone.
