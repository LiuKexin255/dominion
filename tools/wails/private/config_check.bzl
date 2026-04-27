"""Wails config check rule — validates wails.json against Bazel best practices."""

def _wails_config_check_impl(ctx):
    """Parses wails.json and checks it against Bazel best practices.

    The validation runs as a Bazel action (hermetic, no python3 dependency).
    It checks:
    1. frontend:install is empty (Bazel manages dependencies)
    2. frontend:build is empty (Bazel manages the build)
    3. No hooks execute non-Bazel tools in production

    On success, writes a marker file. On failure, the action fails the build.
    """
    wails_json = ctx.file.wails_json
    output = ctx.actions.declare_file(ctx.label.name + ".validated")

    ctx.actions.run_shell(
        inputs = [wails_json],
        outputs = [output],
        command = """set -euo pipefail

WAILS_JSON="{wails_json}"
OUTPUT="{output}"

echo "Checking wails.json: $WAILS_JSON"

ERRORS=0

# Extract a value for a top-level key from simple JSON.
# Handles "key": "value" and "key": "" patterns.
get_val() {{
  local key="$1"
  local file="$2"
  local val
  val=$(sed -n 's/.*"'"$key"'"[[:space:]]*:[[:space:]]*"?\\([^,"]*\\)"?.*/\\1/p' "$file" | head -1 || true)
  echo "$val"
}}

# Check frontend:install — must be empty for Bazel-managed builds
INSTALL=$(get_val "frontend:install" "$WAILS_JSON")
if [ -n "$INSTALL" ]; then
  echo "ERROR: frontend:install must be empty (found: '$INSTALL')"
  ERRORS=$((ERRORS + 1))
fi

# Check frontend:build — must be empty for Bazel-managed builds
BUILD_CMD=$(get_val "frontend:build" "$WAILS_JSON")
if [ -n "$BUILD_CMD" ]; then
  echo "ERROR: frontend:build must be empty (found: '$BUILD_CMD')"
  ERRORS=$((ERRORS + 1))
fi

# Check hooks — look for "hooks" key with non-empty object
if grep -q '"hooks"' "$WAILS_JSON"; then
  HOOKS_LINE=$(grep -A1 '"hooks"' "$WAILS_JSON" | tail -1)
  if echo "$HOOKS_LINE" | grep -qv '^\\s*{{\\s*}}\\s*$\\|^\\s*$'; then
    echo "ERROR: hooks must not execute non-Bazel tools in production"
    ERRORS=$((ERRORS + 1))
  fi
fi

if [ "$ERRORS" -gt 0 ]; then
  echo "FAIL: $ERRORS config check(s) failed"
  exit 1
fi

echo "PASS: wails.json config checks passed"
touch "$OUTPUT"
""".format(
            wails_json = wails_json.path,
            output = output.path,
        ),
        mnemonic = "WailsConfigCheck",
        progress_message = "Validating wails.json: " + wails_json.basename,
    )

    return [DefaultInfo(files = depset([output]))]

_wails_config_check = rule(
    implementation = _wails_config_check_impl,
    attrs = {
        "wails_json": attr.label(
            doc = "The wails.json project config file.",
            allow_single_file = True,
            mandatory = True,
        ),
    },
    doc = "Validates wails.json against Bazel Wails toolchain best practices.",
)

def wails_config_check(name, wails_json, **kwargs):
    """Validates a wails.json file against Bazel best practices.

    The rule checks that frontend:install, frontend:build, and hooks are empty
    or Bazel-aware, preventing config drift that would bypass Bazel-managed builds.

    Args:
        name: Target name.
        wails_json: Label of the wails.json file to validate.
        **kwargs: Additional keyword arguments forwarded to the underlying rule.
    """
    _wails_config_check(
        name = name,
        wails_json = wails_json,
        **kwargs
    )
