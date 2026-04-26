_ZIP_TOOL = Label("//tools/dev/wails:zip_package.py")

def _wails_zip_impl(ctx):
    binary = ctx.file.binary
    ffmpeg = ctx.file.ffmpeg
    input_helper = ctx.file.input_helper
    metadata = ctx.file.metadata
    icon = ctx.file.icon

    output = ctx.actions.declare_file(ctx.attr.name + ".zip")

    entries = [
        "windows-agent.exe=" + binary.path,
        "resources/bin/ffmpeg.exe=" + ffmpeg.path,
        "resources/bin/input-helper.exe=" + input_helper.path,
        "resources/bin/ffmpeg-metadata.json=" + metadata.path,
        "resources/icon.ico=" + icon.path,
    ]

    ctx.actions.run(
        executable = "python3",
        inputs = [binary, ffmpeg, input_helper, metadata, icon],
        outputs = [output],
        arguments = [ctx.file._zip_tool.path, output.path, "."] + entries,
        tools = [ctx.file._zip_tool],
        mnemonic = "WailsZipPackage",
        progress_message = "Creating portable zip: " + output.basename,
    )

    return [DefaultInfo(files = depset([output]))]

_wails_zip = rule(
    implementation = _wails_zip_impl,
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
            allow_single_file = True,
            cfg = "exec",
        ),
    },
)


def wails_windows_package(
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

    _wails_zip(
        name = name,
        binary = binary,
        ffmpeg = ffmpeg,
        input_helper = input_helper,
        metadata = metadata,
        icon = icon,
        **zip_kwargs
    )
