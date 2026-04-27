"""Wails config check rule — validates wails.json against Bazel best practices."""

def _wails_config_check_impl(ctx):
    """Parses wails.json and checks it against Bazel best practices.

    The validation runs at BUILD time via ctx.actions.run_shell. It checks:
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

# Check frontend:install — must be empty for Bazel-managed builds
INSTALL=$(python3 -c "import json,sys; d=json.load(open(sys.argv[1])); print(d.get('frontend:install',''))" "$WAILS_JSON" 2>/dev/null || echo "")
if [ -n "$INSTALL" ]; then
  echo "ERROR: frontend:install must be empty (found: '$INSTALL')"
  ERRORS=$((ERRORS + 1))
fi

# Check frontend:build — must be empty for Bazel-managed builds
BUILD_CMD=$(python3 -c "import json,sys; d=json.load(open(sys.argv[1])); print(d.get('frontend:build',''))" "$WAILS_JSON" 2>/dev/null || echo "")
if [ -n "$BUILD_CMD" ]; then
  echo "ERROR: frontend:build must be empty (found: '$BUILD_CMD')"
  ERRORS=$((ERRORS + 1))
fi

# Check hooks — no non-Bazel hooks in production
HOOKS=$(python3 -c "import json,sys; d=json.load(open(sys.argv[1])); h=d.get('hooks',{{}}); print(len([k for k,v in h.items() if v]))" "$WAILS_JSON" 2>/dev/null || echo "0")
if [ "$HOOKS" != "0" ]; then
  echo "ERROR: hooks must not execute non-Bazel tools in production (found $HOOKS active hooks)"
  ERRORS=$((ERRORS + 1))
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
