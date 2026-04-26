_ZIP_TOOL = Label("//projects/game/windows_agent/release:zip_package")

def _windows_agent_package_impl(ctx):
    binary = ctx.file.binary
    ffmpeg = ctx.file.ffmpeg
    input_helper = ctx.file.input_helper
    metadata = ctx.file.metadata
    icon = ctx.file.icon

    output = ctx.actions.declare_file(ctx.attr.name + ".zip")

    args = ctx.actions.args()
    args.add("-output", output.path)
    args.add("-entry", "windows-agent.exe=" + binary.path)
    args.add("-entry", "resources/bin/ffmpeg.exe=" + ffmpeg.path)
    args.add("-entry", "resources/bin/input-helper.exe=" + input_helper.path)
    args.add("-entry", "resources/bin/ffmpeg-metadata.json=" + metadata.path)
    args.add("-entry", "resources/icon.ico=" + icon.path)
    args.add("-checksum", "resources/bin/ffmpeg.exe.sha256=resources/bin/ffmpeg.exe")

    ctx.actions.run(
        executable = ctx.executable._zip_tool,
        inputs = [binary, ffmpeg, input_helper, metadata, icon],
        outputs = [output],
        arguments = [args],
        mnemonic = "WindowsAgentPackage",
        progress_message = "Creating portable zip: " + output.basename,
    )

    return [DefaultInfo(files = depset([output]))]

_windows_agent_package = rule(
    implementation = _windows_agent_package_impl,
    attrs = {
        "binary": attr.label(
            mandatory = True,
            allow_single_file = True,
            cfg = "target",
        ),
        "ffmpeg": attr.label(
            mandatory = True,
            allow_single_file = True,
        ),
        "input_helper": attr.label(
            mandatory = True,
            allow_single_file = True,
            cfg = "target",
        ),
        "metadata": attr.label(
            mandatory = True,
            allow_single_file = True,
        ),
        "icon": attr.label(
            mandatory = True,
            allow_single_file = True,
        ),
        "_zip_tool": attr.label(
            default = _ZIP_TOOL,
            executable = True,
            cfg = "exec",
        ),
    },
)

def windows_agent_package(
        name,
        binary,
        ffmpeg,
        input_helper,
        metadata,
        icon,
        **kwargs):
    """Packages a pre-built Windows binary with resources into a portable zip.

    Args:
        name: Target name. Produces <name>.zip.
        binary: Label of a go_binary (goos=windows, goarch=amd64).
        ffmpeg: Label of the ffmpeg.exe file.
        input_helper: Label of the input-helper binary (goos=windows).
        metadata: Label of the ffmpeg-metadata.json file.
        icon: Label of the icon.ico file.
    """
    visibility = kwargs.pop("visibility", None)

    zip_kwargs = {}
    if visibility:
        zip_kwargs["visibility"] = visibility

    _windows_agent_package(
        name = name,
        binary = binary,
        ffmpeg = ffmpeg,
        input_helper = input_helper,
        metadata = metadata,
        icon = icon,
        **zip_kwargs
    )
