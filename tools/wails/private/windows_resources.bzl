"""Wails Windows resources rule — generates .syso from icon/manifest/info."""

_GENERATE_WINRES = Label("//tools/wails/helpers:generate_winres")

def _wails_windows_resources_impl(ctx):
    icon = ctx.file.icon
    out = ctx.actions.declare_file(ctx.attr.out)

    args = ctx.actions.args()
    args.add("-icon", icon)
    if ctx.file.manifest:
        args.add("-manifest", ctx.file.manifest)
    if ctx.file.info:
        args.add("-info", ctx.file.info)
    args.add("-out", out)

    inputs = [icon]
    if ctx.file.manifest:
        inputs.append(ctx.file.manifest)
    if ctx.file.info:
        inputs.append(ctx.file.info)

    ctx.actions.run(
        inputs = inputs,
        outputs = [out],
        executable = ctx.executable._winres_tool,
        arguments = [args],
        mnemonic = "WailsWindowsResources",
        progress_message = "Generating Windows resource: " + out.basename,
    )

    return [DefaultInfo(files = depset([out]))]

_wails_windows_resources = rule(
    implementation = _wails_windows_resources_impl,
    attrs = {
        "icon": attr.label(
            doc = "Windows icon (.ico) file.",
            allow_single_file = [".ico"],
            mandatory = True,
        ),
        "manifest": attr.label(
            doc = "Windows manifest (.manifest) file.",
            allow_single_file = [".manifest"],
        ),
        "info": attr.label(
            doc = "Version info JSON file.",
            allow_single_file = [".json"],
        ),
        "out": attr.string(
            doc = "Output .syso filename.",
            default = "windows-res.syso",
        ),
        "_winres_tool": attr.label(
            default = _GENERATE_WINRES,
            executable = True,
            cfg = "exec",
        ),
    },
    doc = "Generates a Windows .syso resource file from icon, manifest, and version info.",
)

def wails_windows_resources(name, icon, out = "windows-res.syso", manifest = None, info = None, **kwargs):
    """Generates Windows resources for Wails applications.

    Args:
        name: Target name.
        icon: Label of a Windows .ico file.
        out: Output .syso filename.
        manifest: Optional label of a Windows .manifest file.
        info: Optional label of a version info JSON file.
        **kwargs: Additional keyword arguments forwarded to the underlying rule.
    """
    _wails_windows_resources(
        name = name,
        icon = icon,
        out = out,
        manifest = manifest,
        info = info,
        **kwargs
    )
