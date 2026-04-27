"""Wails CLI toolchain definitions."""

load("//tools/wails/private:providers.bzl", "WailsToolchainInfo")

def _wails_cli_toolchain_impl(ctx):
    """Creates a WailsToolchainInfo from the toolchain configuration."""
    runfiles = ctx.runfiles(files = [ctx.file.wails])
    return [
        platform_common.ToolchainInfo(
            wails_toolchain = WailsToolchainInfo(
                wails = ctx.file.wails,
                version = ctx.attr.version,
                runfiles = runfiles,
            ),
        ),
    ]

_wails_cli_toolchain = rule(
    implementation = _wails_cli_toolchain_impl,
    attrs = {
        "wails": attr.label(
            doc = "The Wails CLI executable",
            allow_single_file = True,
            mandatory = True,
            cfg = "exec",
        ),
        "version": attr.string(
            doc = "Pinned Wails CLI version (e.g. v2.12.0)",
            mandatory = True,
        ),
    },
    doc = "Declares a Wails CLI toolchain with a pinned Wails executable and version.",
)

def wails_cli_toolchain(name, wails, version, **kwargs):
    """Registers a Wails CLI toolchain.

    Creates an internal toolchain implementation target and wraps it with
    the toolchain() rule for the given platform constraints.

    Args:
        name: Base name for the toolchain targets.
        wails: Label of the Wails CLI executable.
        version: Pinned Wails version string.
        **kwargs: Additional attributes passed to toolchain() (e.g. exec_compatible_with).
    """
    impl_name = name + "_impl"
    _wails_cli_toolchain(
        name = impl_name,
        wails = wails,
        version = version,
        visibility = ["//visibility:public"],
    )

    native.toolchain(
        name = name,
        toolchain = impl_name,
        toolchain_type = "//tools/wails:toolchain_type",
        visibility = ["//visibility:public"],
        **kwargs
    )
