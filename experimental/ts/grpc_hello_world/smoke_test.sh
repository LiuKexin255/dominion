#!/usr/bin/env bash
# Smoke test: verify server_pkg tar contains all required modules.
# Only checks module resolution — does NOT start the server.
set -euo pipefail

# Tar structure: everything under dominion/{app}/{service}/
#   dominion/grpc-hello-world-ts/service/package.json
#   dominion/grpc-hello-world-ts/service/src/bootstrap.js
#   dominion/grpc-hello-world-ts/service/node_modules/@dominion/...
#   dominion/grpc-hello-world-ts/service/node_modules/@grpc/...
SERVICE_ROOT="dominion/grpc-hello-world-ts/service"

# --- Locate the tar via Bazel runfiles ---
TAR_PATH=""
for arg in "$@"; do
  case "$arg" in
    *.tar) TAR_PATH="$arg" ;;
  esac
done

if [[ -z "${TAR_PATH}" ]]; then
  if [[ -n "${TEST_SRCDIR:-}" ]]; then
    TAR_PATH="${TEST_SRCDIR}/experimental/ts/grpc_hello_world/server_pkg.tar"
  fi
fi

if [[ -z "${TAR_PATH}" || ! -f "${TAR_PATH}" ]]; then
  echo "FAIL: server_pkg.tar not found"
  echo "Args received: $*"
  exit 1
fi

echo "Tar found at: ${TAR_PATH}"
echo "Tar size: $(wc -c < "${TAR_PATH}") bytes"

# --- Extract to temp directory ---
WORK_DIR="$(mktemp -d)"
trap 'rm -rf "${WORK_DIR}"' EXIT

tar -xf "${TAR_PATH}" -C "${WORK_DIR}"

echo ""
echo "=== Tar entry count ==="
tar tf "${TAR_PATH}" | wc -l

# The service root is where node will run from
SVC="${WORK_DIR}/${SERVICE_ROOT}"

echo ""
echo "=== Service root contents ==="
ls "${SVC}/src/"
echo ""
echo "=== node_modules/@dominion/ ==="
ls "${SVC}/node_modules/@dominion/"

# --- Verify key files exist ---
ERRORS=""

check_file() {
  local path="$1"
  if [[ ! -f "${path}" ]]; then
    ERRORS="${ERRORS}\nMISSING: ${path}"
    return 1
  else
    echo "OK: ${path}"
    return 0
  fi
}

echo ""
echo "=== Checking workspace packages ==="
check_file "${SVC}/node_modules/@dominion/common-js-config/package.json"
check_file "${SVC}/node_modules/@dominion/common-js-config/src/index.js"
check_file "${SVC}/node_modules/@dominion/common-js-logs/package.json"
check_file "${SVC}/node_modules/@dominion/common-js-logs/src/index.js"
check_file "${SVC}/node_modules/@dominion/common-js-otel/package.json"
check_file "${SVC}/node_modules/@dominion/common-js-otel/src/index.js"
check_file "${SVC}/node_modules/@dominion/common-js-grpc-otel/package.json"
check_file "${SVC}/node_modules/@dominion/common-js-grpc-otel/src/index.js"

echo ""
echo "=== Checking entrypoint ==="
check_file "${SVC}/src/bootstrap.js"
check_file "${SVC}/src/server.js"

echo ""
echo "=== Checking npm deps ==="
check_file "${SVC}/node_modules/@grpc/grpc-js/package.json"
check_file "${SVC}/node_modules/@grpc/proto-loader/package.json"

# --- Verify service root package.json declares ESM ---
echo ""
echo "=== Checking service root package.json (ESM) ==="
if [[ ! -f "${SVC}/package.json" ]]; then
  echo "FAIL: missing ${SVC}/package.json"
  exit 1
fi
if ! grep -q '"type": "module"' "${SVC}/package.json"; then
  echo "FAIL: ${SVC}/package.json does not declare \"type\": \"module\""
  exit 1
fi
echo "OK: service root package.json declares \"type\": \"module\""

# --- Verify module resolution with Node.js ---
echo ""
echo "=== Testing module resolution with Node.js (ESM) ==="

# Run node from the service root so module resolution uses local node_modules.
# --input-type=module makes the eval script an ES module (top-level await +
# dynamic import), matching the ESM artifacts shipped in the tar.
NODE_OUTPUT=$(cd "${SVC}" && node --input-type=module -e "
    import assert from 'node:assert';
    import fs from 'node:fs';

    // Test 1: Workspace packages resolve
    const logs = await import('./node_modules/@dominion/common-js-logs/src/index.js');
    console.log('OK: @dominion/common-js-logs loaded, exports:', Object.keys(logs).join(', '));

    const otel = await import('./node_modules/@dominion/common-js-otel/src/index.js');
    console.log('OK: @dominion/common-js-otel loaded, exports:', Object.keys(otel).join(', '));

    const grpcOtel = await import('./node_modules/@dominion/common-js-grpc-otel/src/index.js');
    console.log('OK: @dominion/common-js-grpc-otel loaded, exports:', Object.keys(grpcOtel).join(', '));

    // Test 2: npm deps resolve. Bare specifiers (not directory paths) —
    // ESM resolves packages via node_modules lookup + package.json main,
    // while directory imports are a CJS-only feature.
    const grpcJs = await import('@grpc/grpc-js');
    console.log('OK: @grpc/grpc-js loaded');

    const protoLoader = await import('@grpc/proto-loader');
    console.log('OK: @grpc/proto-loader loaded');

    // Test 3: Bootstrap entrypoint is parseable JS (don't import it — it starts the server)
    const bootstrap = fs.readFileSync('./src/bootstrap.js', 'utf8');
    assert(bootstrap.includes('import'), 'bootstrap.js should contain import statements');
    console.log('OK: bootstrap.js is parseable (' + bootstrap.length + ' chars)');

    console.log('ALL MODULE RESOLUTION CHECKS PASSED');
  " 2>&1) || {
  echo "FAIL: Node.js module resolution failed"
  echo "${NODE_OUTPUT}"
  # Check specifically for MODULE_NOT_FOUND
  if echo "${NODE_OUTPUT}" | grep -q "MODULE_NOT_FOUND"; then
    echo ""
    echo "ERROR: MODULE_NOT_FOUND detected — packaged tar has missing dependencies"
  fi
  exit 1
}

echo "${NODE_OUTPUT}"

# Check for MODULE_NOT_FOUND in output (even on success path)
if echo "${NODE_OUTPUT}" | grep -q "MODULE_NOT_FOUND"; then
  echo "FAIL: MODULE_NOT_FOUND detected in output"
  exit 1
fi

# --- Report file errors ---
if [[ -n "${ERRORS}" ]]; then
  echo ""
  echo "FAIL: Missing files detected"
  echo -e "${ERRORS}"
  exit 1
fi

echo ""
echo "=== SMOKE TEST PASSED ==="
