#!/usr/bin/env bash
set -u

ROOT=${1:-testdata/alpine-x86}
BIN=${2:-/tmp/ishrun-matrix}
STEPS=${STEPS:-18000000}

if [[ ! -d "$ROOT" ]]; then
  echo "busybox matrix: rootfs not found: $ROOT" >&2
  exit 2
fi
if [[ $# -lt 2 && ! -x "$BIN" ]]; then
  repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
  echo "busybox matrix: building $BIN" >&2
  if ! (cd "$repo_dir" && go build -trimpath -o "$BIN" ./cmd/ishrun); then
    echo "busybox matrix: build failed" >&2
    exit 2
  fi
fi
if [[ ! -x "$BIN" ]]; then
  echo "busybox matrix: runner not executable: $BIN" >&2
  exit 2
fi

cases=(
  'true'
  'echo|hello|from|busybox'
  'test|-f|/etc/os-release'
  'cat|/etc/os-release'
  'head|-n|2|/etc/os-release'
  'wc|-l|/etc/os-release'
  'whoami'
  'id'
  'pwd'
  'ls|/bin'
  'readlink|/bin/sh'
  'basename|/etc/os-release'
  'dirname|/etc/os-release'
  'env'
  'cut|-d:|-f1|/etc/passwd'
  'sort|/etc/passwd'
  'printf|%s\\n|printf-ok'
  'date'
  'uname|-a'
  'stat|/etc/os-release'
  'sleep|0'
  'sleep|0.5'
  'grep|NAME|/etc/os-release'
  'uniq|/etc/passwd'
  'md5sum|/etc/os-release'
  'sha256sum|/etc/os-release'
  'od|-An|-tx1|/etc/os-release'
  'find|/etc|-maxdepth|1'
  'df|/etc'
  'du|/etc/os-release'
  'ps'
  'free'
  'which|sh'
  'readlink|-f|/bin/sh'
  'sh|-c|echo pipe-ok __PIPE__ wc -c'
)

failures=0
for spec in "${cases[@]}"; do
  IFS='|' read -r -a argv <<< "$spec"
  if [[ "${argv[0]:-}" == 'sh' && "${argv[1]:-}" == '-c' && "${argv[2]:-}" == *__PIPE__* ]]; then
    argv[2]=${argv[2]//__PIPE__/|}
  fi
  rc=0
  output=$(timeout 30s "$BIN" -root "$ROOT" -steps "$STEPS" "$ROOT/bin/busybox" "${argv[@]}" 2>&1) || rc=$?
  compact=$(printf '%s' "$output" | tr '\n' ' ' | cut -c1-180)
  printf '%-28s rc=%-3d %s\n' "$spec" "$rc" "$compact"
  if [[ $rc -ne 0 ]]; then
    failures=$((failures + 1))
  fi
done
printf 'busybox matrix: %d/%d failed\n' "$failures" "${#cases[@]}"
if [[ $failures -ne 0 ]]; then
  exit 1
fi
