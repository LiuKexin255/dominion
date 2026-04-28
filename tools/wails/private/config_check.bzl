"""Wails config check rule — validates wails.json against Bazel best practices."""

_HERMETIC_ENV = {
    "HOME": "/tmp/wails_action_home",
    "TMPDIR": "/tmp",
    "NO_COLOR": "1",
}

def _wails_config_check_impl(ctx):
    """Parses wails.json and checks it against Bazel best practices.

    The validation runs as a Bazel action (hermetic Go helper).
    It checks:
    1. frontend:install is empty (Bazel manages dependencies)
    2. frontend:build is empty (Bazel manages the build)
    3. No hooks execute non-Bazel tools in production

    On success, writes a marker file. On failure, the action fails the build.
    """
    wails_json = ctx.file.wails_json
    output = ctx.actions.declare_file(ctx.label.name + ".validated")

    ctx.actions.run(
        executable = ctx.executable._inspect_tool,
        inputs = [wails_json],
        outputs = [output],
        arguments = [
            "-wails_json",
            wails_json.path,
            "-out",
            output.path,
        ],
        env = _HERMETIC_ENV,
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
        "_inspect_tool": attr.label(
            default = Label("//tools/wails/helpers/inspect_config:inspect_wails_config"),
            executable = True,
            cfg = "exec",
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
