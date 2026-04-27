"""Diagnostic rules for Wails toolchain health checks."""

def _wails_version_test_impl(ctx):
    """Test that verifies the Wails CLI version matches the pinned version."""
    wails = ctx.toolchains["//tools/wails:toolchain_type"].wails_toolchain.wails
    version = ctx.toolchains["//tools/wails:toolchain_type"].wails_toolchain.version

    output = ctx.actions.declare_file(ctx.label.name + ".sh")
    ctx.actions.write(
        output = output,
        content = """\
#!/usr/bin/env bash
set -euo pipefail
WAILS="${RUNFILES_DIR}/${TEST_WORKSPACE}/%s"
EXPECTED="%s"
ACTUAL="$("$WAILS" version 2>&1 || true)"
echo "Wails CLI version output: $ACTUAL"
echo "Expected version: $EXPECTED"
if echo "$ACTUAL" | grep -q "$EXPECTED"; then
  echo "PASS: Wails CLI version matches expected $EXPECTED"
  exit 0
else
  echo "FAIL: Wails CLI version does not contain expected $EXPECTED"
  exit 1
fi
""" % (wails.short_path, version),
        is_executable = True,
    )

    return [
        DefaultInfo(
            executable = output,
            runfiles = ctx.runfiles(files = [wails]),
        ),
    ]

_wails_version_test = rule(
    implementation = _wails_version_test_impl,
    test = True,
    toolchains = ["//tools/wails:toolchain_type"],
    doc = "Verifies that the Wails CLI binary outputs the expected version string.",
)

def wails_version_test(name, **kwargs):
    _wails_version_test(
        name = name,
        **kwargs
    )

def _wails_doctor_impl(ctx):
    """Run wails doctor diagnostics (manual only)."""
    wails = ctx.toolchains["//tools/wails:toolchain_type"].wails_toolchain.wails

    output = ctx.actions.declare_file(ctx.label.name + ".sh")
    ctx.actions.write(
        output = output,
        content = """\
#!/usr/bin/env bash
set -euo pipefail
WAILS="${RUNFILES_DIR:-$0.runfiles}/${TEST_WORKSPACE:-_main}/%s"
echo "Running wails doctor..."
exec "$WAILS" doctor
""" % wails.short_path,
        is_executable = True,
    )

    return [
        DefaultInfo(
            executable = output,
            runfiles = ctx.runfiles(files = [wails]),
        ),
    ]

_wails_doctor = rule(
    implementation = _wails_doctor_impl,
    executable = True,
    toolchains = ["//tools/wails:toolchain_type"],
    doc = "Runs wails doctor diagnostics. Manual use only.",
)

def wails_doctor(name, **kwargs):
    kwargs.setdefault("tags", [])
    if "manual" not in kwargs["tags"]:
        kwargs["tags"].append("manual")
    _wails_doctor(
        name = name,
        **kwargs
    )
