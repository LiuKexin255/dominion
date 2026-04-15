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

prefix="${PREFIX:-}"

while (($# > 0)); do
  case "$1" in
    --prefix)
      if (($# < 2)); then
        echo "ERROR: --prefix requires a path" >&2
        exit 1
      fi
      prefix="$2"
      shift 2
      ;;
    --prefix=*)
      prefix="${1#--prefix=}"
      shift
      ;;
    *)
      echo "ERROR: unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

if [[ -z "$prefix" ]]; then
  if [[ -n "${HOME:-}" ]]; then
    prefix="$HOME/.local"
  else
    echo "ERROR: HOME is not set; pass --prefix <path> or set PREFIX" >&2
    exit 1
  fi
fi

src=""
for candidate in \
  "_main/tools/deploy/v2/deploy_/deploy" \
  "__main__/tools/deploy/v2/deploy_/deploy" \
  "tools/deploy/v2/deploy_/deploy" \
  "_main/tools/deploy/v2/deploy" \
  "__main__/tools/deploy/v2/deploy" \
  "tools/deploy/v2/deploy"; do
  resolved="$(rlocation "$candidate" 2>/dev/null || true)"
  if [[ -n "$resolved" && -f "$resolved" ]]; then
    src="$resolved"
    break
  fi
done

if [[ -z "$src" ]]; then
  echo "ERROR: deploy binary not found in runfiles" >&2
  exit 1
fi

mkdir -p "$prefix/bin"

dest="$prefix/bin/deploy"
tmp="$(mktemp "$prefix/bin/.deploy.tmp.XXXXXX")"

cleanup() {
  if [[ -n "${tmp:-}" && -e "$tmp" ]]; then
    rm -f "$tmp"
  fi
}
trap cleanup EXIT

cp "$src" "$tmp"
chmod 0755 "$tmp"
mv -f "$tmp" "$dest"
trap - EXIT

echo "Installed deploy to $dest"
