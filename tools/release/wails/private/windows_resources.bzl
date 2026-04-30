"""Wails Windows resources rule.

Generates a Windows .syso resource file from icon, manifest, and version info
inputs using the Go winres library. The produced .syso can be consumed by
wails_go_binary via the srcs attribute.
"""

def _wails_windows_resources_impl(ctx):
    """Implementation of wails_windows_resources rule."""
    out = ctx.actions.declare_file(ctx.attr.out)

    icon_path = ctx.file.icon.path if ctx.file.icon else ""
    manifest_path = ctx.file.manifest.path if ctx.file.manifest else ""
    info_path = ctx.file.info.path if ctx.file.info else ""

    inputs = []
    if ctx.file.icon:
        inputs.append(ctx.file.icon)
    if ctx.file.manifest:
        inputs.append(ctx.file.manifest)
    if ctx.file.info:
        inputs.append(ctx.file.info)

    args = ctx.actions.args()
    args.add("generate_winres")
    args.add(icon_path)
    args.add(manifest_path)
    args.add(info_path)
    args.add(ctx.attr.arch)
    args.add(out.path)

    ctx.actions.run(
        executable = ctx.executable._helper,
        arguments = [args],
        inputs = inputs,
        outputs = [out],
        mnemonic = "WailsWinres",
        progress_message = "Generating Windows resources %{label}",
    )

    return [DefaultInfo(files = depset([out]))]

wails_windows_resources = rule(
    implementation = _wails_windows_resources_impl,
    attrs = {
        "icon": attr.label(
            allow_single_file = [".ico"],
            doc = "Windows .ico icon file to embed in the resource.",
        ),
        "manifest": attr.label(
            allow_single_file = [".manifest", ".xml"],
            doc = "Windows application manifest XML file.",
        ),
        "info": attr.label(
            allow_single_file = [".json"],
            doc = "Version info JSON file with FileVersion, ProductVersion, etc.",
        ),
        "arch": attr.string(
            default = "amd64",
            doc = "Target architecture: amd64, arm64, 386, or arm.",
        ),
        "out": attr.string(
            mandatory = True,
            doc = "Output .syso filename (e.g. resource_windows_amd64.syso).",
        ),
        "_helper": attr.label(
            default = "//tools/release/wails/helpers:helpers",
            executable = True,
            cfg = "exec",
            doc = "Helper binary that generates the .syso file.",
        ),
    },
    doc = "Generates a Windows .syso resource file from icon, manifest, and version info.",
)
