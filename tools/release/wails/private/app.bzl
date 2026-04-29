"""Wails application macro."""

load("//tools/release/wails/private:go_binary.bzl", "wails_go_binary")
load("//tools/release/wails/private:providers.bzl", "WailsAppInfo", "WailsAssetsInfo")

def _wails_app_info_impl(ctx):
    binary = ctx.file.binary

    assets_info = None
    if ctx.attr.assets:
        assets_info = ctx.attr.assets[WailsAssetsInfo]

    return [
        DefaultInfo(files = depset([binary])),
        WailsAppInfo(
            binary = binary,
            assets = assets_info,
            bindings = None,
            resources = ctx.file.resources if ctx.file.resources else None,
            platform = ctx.attr.platform,
        ),
    ]

_wails_app_info = rule(
    implementation = _wails_app_info_impl,
    attrs = {
        "binary": attr.label(mandatory = True, allow_single_file = True),
        "assets": attr.label(providers = [WailsAssetsInfo]),
        "resources": attr.label(allow_single_file = True, default = None),
        "platform": attr.string(mandatory = True),
    },
    provides = [WailsAppInfo],
)

def wails_app(
        name,
        binary_name,
        platform,
        go_library,
        assets,
        resources = None,
        bindings = None,
        webview2 = "embed",
        visibility = None,
        **kwargs):
    """Aggregates Bazel-built Wails components into an application target."""
    if platform != "windows/amd64":
        fail("wails_app only supports windows/amd64, got: " + platform)

    wails_go_binary(
        name = name + "_binary",
        binary_name = binary_name,
        platform = platform,
        embed = [go_library],
        resources = resources,
        webview2 = webview2,
        **kwargs
    )

    _wails_app_info(
        name = name,
        binary = name + "_binary",
        assets = assets,
        resources = resources,
        platform = platform,
        visibility = visibility,
    )
