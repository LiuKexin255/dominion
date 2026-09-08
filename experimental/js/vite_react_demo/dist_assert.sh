#!/usr/bin/env bash
# Dist artifact assertions for the vite React demo.
#
# Contract: specs/050-vite-react-bazel/contracts/dist-artifact-assertions.md
# (target wiring precedent: experimental/js/grpc_hello_world/BUILD.bazel).
# Input: argv[1] = path of the :dist tree artifact ($(location :dist)).
# Output: one PASS line per assertion (A1..A4); any failure prints FAIL plus
# the assertion id and exits non-zero — never warn-and-continue.
set -euo pipefail

# Single-source constant shared with experimental/js/vite_react_demo/src/App.tsx
# (contract behaviour requirement 4): the component renders MARKER verbatim and
# this script greps the same literal; change both together.
readonly MARKER='dominion-vite-react-demo'
# React runtime trace kept in bundles by vite's default legal-comments policy:
# the "@license React react-dom.production.min.js" banner survives minification
# (verified against the actual bundle).
readonly REACT_RUNTIME_TRACE='react-dom.production.min.js'

DIST_DIR="${1:-}"
if [[ -z "${DIST_DIR}" || ! -d "${DIST_DIR}" ]]; then
  echo "FAIL: argv[1] must be the dist directory, got: '${DIST_DIR}'"
  exit 1
fi

# --- A1: entry HTML exists and is non-empty ---------------------------------
INDEX_HTML="${DIST_DIR}/index.html"
if [[ ! -s "${INDEX_HTML}" ]]; then
  echo "FAIL A1: ${INDEX_HTML} missing or empty"
  exit 1
fi
echo "PASS A1: index.html exists and is non-empty"

# --- A2: every root-relative src=/href= reference resolves inside the dist --
# Non-local references (https:, data:, anchors) are out of scope by contract;
# anything starting with '/' names a file that must exist in the dist tree.
dangling=""
while IFS= read -r ref; do
  [[ "${ref}" == /* ]] || continue
  if [[ ! -f "${DIST_DIR}${ref}" ]]; then
    dangling="${dangling}
  DANGLING: ${ref}"
  fi
done < <(
  grep -oE '(src|href)="[^"]*"' "${INDEX_HTML}" \
    | sed -E 's/^(src|href)="([^"]*)"/\2/' \
    | sort -u
)

if [[ -n "${dangling}" ]]; then
  echo "FAIL A2: index.html references missing dist resources:${dangling}"
  exit 1
fi
echo "PASS A2: all src=/href= product-local references resolve (zero dangling)"

# --- A3: a content-hashed JS asset exists under assets/ ----------------------
# Rollup/vite content hashes are 8-char url-safe base64 (e.g. index-CZHFTCDt.js),
# not hexadecimal — hence [0-9A-Za-z_-], per the contract's assertion list (A3)
# at specs/050-vite-react-bazel/contracts/dist-artifact-assertions.md.
shopt -s nullglob
assets_dir="${DIST_DIR}/assets"
[[ -d "${assets_dir}" ]] || assets_dir="${DIST_DIR}"
hashed_js_count=0
for f in "${assets_dir}"/*-[0-9A-Za-z_-][0-9A-Za-z_-][0-9A-Za-z_-][0-9A-Za-z_-][0-9A-Za-z_-][0-9A-Za-z_-][0-9A-Za-z_-][0-9A-Za-z_-].js; do
  [[ -f "${f}" ]] && hashed_js_count=$((hashed_js_count + 1))
done

if ((hashed_js_count == 0)); then
  echo "FAIL A3: no *-<hash>.js asset under ${DIST_DIR}/assets/"
  exit 1
fi
echo "PASS A3: content-hashed JS asset(s) present (${hashed_js_count})"

# --- A4: demo component logic and the React runtime really got bundled ------
# -L so tree-artifact members presented as symlinks (the sh_test sandbox
# layout) still resolve to regular files for -type f.
mapfile -d '' js_files < <(find -L "${DIST_DIR}" -type f -name '*.js' -print0)
marker_found=0
runtime_found=0
for f in "${js_files[@]}"; do
  if ((marker_found == 0)) && grep -qF "${MARKER}" "${f}"; then
    marker_found=1
  fi
  if ((runtime_found == 0)) && grep -qF "${REACT_RUNTIME_TRACE}" "${f}"; then
    runtime_found=1
  fi
done

if ((marker_found == 0)); then
  echo "FAIL A4: marker string '${MARKER}' not found in any bundled JS"
  exit 1
fi
if ((runtime_found == 0)); then
  echo "FAIL A4: React runtime trace '${REACT_RUNTIME_TRACE}' not found in any bundled JS"
  exit 1
fi
echo "PASS A4: marker string and react-dom runtime trace found in bundles"

echo "ALL DIST ASSERTIONS PASSED (A1-A4)"
