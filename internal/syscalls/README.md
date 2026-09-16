# Linux i386 syscall units

Each subdirectory contains one file per syscall and groups related ABI operations.

The categories are `process`, `filesystem`, `io`, `memory`, `signals`, `time`, `futex`, `identity`, `metadata`, and `random`.

The dispatcher remains in `internal/kernel/syscall.go`; these units provide the stable per-call catalog and numeric ABI identity.
