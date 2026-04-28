"""Wails app aggregation macro — orchestrates Wails build phases."""

load("//tools/wails/private:bindings.bzl", "wails_bindings")
load("//tools/wails/private:frontend_assets.bzl", "wails_frontend_assets")
load("//tools/wails/private:providers.bzl", "WailsAppInfo")
load("//tools/wails/private:windows_resources.bzl", "wails_windows_resources")

def _optional_output(target):
    if not target:
        return None

    files = target[DefaultInfo].files.to_list()
    if not files:
        return None

    if len(files) > 1:
        fail("wails_app phase target must provide exactly one output")

    return files[0]

def _wails_app_impl(ctx):
    bindings = _optional_output(ctx.attr.bindings)
    frontend_assets = _optional_output(ctx.attr.frontend_assets)
    resources = _optional_output(ctx.attr.windows_resources)
    outputs = []

    if bindings:
        outputs.append(bindings)
    if frontend_assets:
        outputs.append(frontend_assets)
    if resources:
        outputs.append(resources)

    return [
        DefaultInfo(files = depset(outputs)),
        WailsAppInfo(
            binary = None,
            frontend_assets = frontend_assets,
            bindings = bindings,
            resources = resources,
        ),
    ]

_wails_app = rule(
    implementation = _wails_app_impl,
    attrs = {
        "bindings": attr.label(
            doc = "Optional generated Wails bindings target.",
        ),
        "frontend_assets": attr.label(
            doc = "Optional re-rooted frontend assets target.",
        ),
        "windows_resources": attr.label(
            doc = "Optional generated Windows resources target.",
        ),
    },
    doc = "Aggregates Wails application phase outputs and exposes WailsAppInfo.",
)

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
    bindings = None
    frontend_assets = None
    windows_resources = None

    if go_srcs:
        bindings_name = name + "_bindings"
        wails_bindings(
            name = bindings_name,
            go_srcs = go_srcs,
            wails_json = wails_json,
            tags = bindings_tags or [],
            **kwargs
        )
        bindings = ":" + bindings_name

    if frontend:
        frontend_assets_name = name + "_frontend_assets"
        wails_frontend_assets(
            name = frontend_assets_name,
            src = frontend,
            out = "frontend_dist",
            **kwargs
        )
        frontend_assets = ":" + frontend_assets_name

    if icon:
        windows_resources_name = name + "_windows_resources"
        wails_windows_resources(
            name = windows_resources_name,
            icon = icon,
            out = name + "-res.syso",
            **kwargs
        )
        windows_resources = ":" + windows_resources_name

    _wails_app(
        name = name,
        bindings = bindings,
        frontend_assets = frontend_assets,
        windows_resources = windows_resources,
        **kwargs
    )
