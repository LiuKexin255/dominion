"""Wails Go binary macro."""

load("@rules_go//go:def.bzl", "go_binary")

def wails_go_binary(
        name,
        binary_name,
        platform,
        embed,
        resources = None,
        webview2 = "embed",
        visibility = None,
        **kwargs):
    """Builds a Windows Wails production Go binary."""
    if platform != "windows/amd64":
        fail("wails_go_binary only supports windows/amd64, got: " + platform)

    valid_webview2 = ["download", "embed", "browser", "error"]
    if webview2 not in valid_webview2:
        fail("wails_go_binary: webview2 must be one of %s, got: %s" % (valid_webview2, webview2))

    srcs = []
    if resources:
        srcs.append(resources)

    go_binary(
        name = name,
        embed = embed,
        srcs = srcs,
        goos = "windows",
        goarch = "amd64",
        pure = "on",
        gotags = [
            "production",
            "wv2runtime." + webview2,
        ],
        gc_linkopts = [
            "-w",
            "-s",
            "-H",
            "windowsgui",
        ],
        out = binary_name + ".exe",
        visibility = visibility,
        **kwargs
    )
