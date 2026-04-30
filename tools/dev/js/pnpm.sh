#!/usr/bin/env bash

set -euo pipefail

set +e
f=bazel_tools/tools/bash/runfiles/runfiles.bash
source "${RUNFILES_DIR:-/dev/null}/$f" 2>/dev/null || \
  source "$(grep -sm1 "^$f " "${RUNFILES_MANIFEST_FILE:-/dev/null}" | cut -f2- -d' ')" 2>/dev/null || \
  source "$0.runfiles/$f" 2>/dev/null || \
  source "$(grep -sm1 "^$f " "$0.runfiles_manifest" | cut -f2- -d' ')" 2>/dev/null || \
  source "$(grep -sm1 "^$f " "$0.exe.runfiles_manifest" | cut -f2- -d' ')" 2>/dev/null || {
    echo "ERROR: cannot find $f" >&2
    exit 1
  }
set -e

pnpm_bin=""
for candidate in \
  "aspect_rules_js++pnpm+pnpm/pnpm_/pnpm"; do
  resolved="$(rlocation "$candidate" 2>/dev/null || true)"
  if [[ -n "$resolved" && -f "$resolved" ]]; then
    pnpm_bin="$resolved"
    break
  fi
done

if [[ -z "$pnpm_bin" ]]; then
  echo "ERROR: pnpm binary not found in runfiles" >&2
  exit 1
fi

exec "$pnpm_bin" --dir "${BUILD_WORKSPACE_DIRECTORY}" "$@"
