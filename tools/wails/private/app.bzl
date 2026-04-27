"""Wails app aggregation macro — orchestrates Wails build phases."""

load("//tools/wails/private:bindings.bzl", "wails_bindings")
load("//tools/wails/private:frontend_assets.bzl", "wails_frontend_assets")
load("//tools/wails/private:windows_resources.bzl", "wails_windows_resources")

def wails_app(
        name,
        wails_json,
        frontend = None,
        go_srcs = None,
        icon = None,
        bindings_tags = None,
        **kwargs):
    """Aggregation macro that wires up Wails build phases.

    Creates a dependency graph:
      bindings (optional) → frontend_assets (optional) → windows_resources (optional)

    Args:
        name: Base name for generated sub-targets.
        wails_json: Label of the wails.json project config.
        frontend: Optional label of the frontend build output (tree artifact).
        go_srcs: Optional list of Go source labels for bindings generation.
        icon: Optional label of a Windows .ico file.
        bindings_tags: Optional build tags for bindings generation.
        **kwargs: Additional keyword arguments passed to sub-rules and aggregate target.
    """
    srcs = []

    if go_srcs:
        bindings_name = name + "_bindings"
        wails_bindings(
            name = bindings_name,
            go_srcs = go_srcs,
            wails_json = wails_json,
            tags = bindings_tags or [],
            **kwargs
        )
        srcs.append(":" + bindings_name)

    if frontend:
        frontend_assets_name = name + "_frontend_assets"
        wails_frontend_assets(
            name = frontend_assets_name,
            src = frontend,
            out = "frontend_dist",
            **kwargs
        )
        srcs.append(":" + frontend_assets_name)

    if icon:
        windows_resources_name = name + "_windows_resources"
        wails_windows_resources(
            name = windows_resources_name,
            icon = icon,
            out = name + "-res.syso",
            **kwargs
        )
        srcs.append(":" + windows_resources_name)

    native.filegroup(
        name = name,
        srcs = srcs,
        **kwargs
    )
