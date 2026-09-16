# Release Notes v9

The syscall dispatcher now executes several operations from independent categorized packages.

Extracted implementations include process identifiers and groups, `set_tid_address`, UID/GID identity, `gettimeofday`, `clock_gettime`, `getrandom`, `sysinfo`, and `uname`.

The extracted functions are real runtime handlers, not only numeric catalogs. The dispatcher calls them directly, and the old duplicate implementations were removed from `syscall.go`.

All new operation files remain below 20 lines and `go test ./...` passes.

The remaining filesystem, I/O, memory, signal, futex, and instruction handlers will be migrated in small verified batches. The large CPU and kernel files are intentionally retained until each extraction preserves ABI behavior.
