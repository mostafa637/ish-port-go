#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
failed=0
while IFS= read -r file; do
  lines=$(wc -l < "$file")
  if (( lines > 20 )); then
    printf 'too many lines (%s): %s\n' "$lines" "$file" >&2
    failed=1
  fi
done < <(find "$root/internal/instructions" "$root/internal/syscalls" -type f -name '*.go' -print)
exit "$failed"
