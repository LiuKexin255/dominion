"""Wails frontend asset library macro."""

load("@rules_go//go:def.bzl", "go_library")
load("//tools/release/wails/private:providers.bzl", "WailsAssetsInfo")

def _wails_assets_provider_impl(ctx):
    return [
        WailsAssetsInfo(
            library = ctx.attr.library,
            importpath = ctx.attr.importpath,
        ),
    ]

_wails_assets_provider = rule(
    implementation = _wails_assets_provider_impl,
    attrs = {
        "library": attr.label(mandatory = True),
        "importpath": attr.string(mandatory = True),
    },
    provides = [WailsAssetsInfo],
)

def _stage_assets_impl(ctx):
    staged_dir = ctx.actions.declare_directory(ctx.attr.out)
    inputs = ctx.files.src
    if len(inputs) != 1:
        fail("_stage_assets src must provide exactly one directory artifact")

    ctx.actions.run(
        inputs = inputs,
        outputs = [staged_dir],
        executable = ctx.executable._helper,
        arguments = [
            "stage_frontend",
            inputs[0].path,
            staged_dir.path,
        ],
        mnemonic = "WailsStageAssets",
        progress_message = "Staging Wails frontend assets for %{label}",
    )

    return [DefaultInfo(files = depset([staged_dir]))]

_stage_assets = rule(
    implementation = _stage_assets_impl,
    attrs = {
        "src": attr.label(
            mandatory = True,
            allow_files = True,
        ),
        "out": attr.string(default = "frontend_dist"),
        "_helper": attr.label(
            default = "//tools/release/wails/helpers:helpers",
            executable = True,
            cfg = "exec",
        ),
    },
)

def _generate_assets_file_impl(ctx):
    assets_go = ctx.actions.declare_file("assets.go")

    ctx.actions.run(
        outputs = [assets_go],
        executable = ctx.executable._helper,
        arguments = [
            "generate_assets_go",
            ctx.attr.variable_name,
            ctx.attr.embed_dir,
            assets_go.path,
            ctx.attr.package_name,
        ],
        mnemonic = "WailsGenerateAssetsGo",
        progress_message = "Generating Wails assets.go for %{label}",
    )

    return [DefaultInfo(files = depset([assets_go]))]

_generate_assets_file = rule(
    implementation = _generate_assets_file_impl,
    attrs = {
        "variable_name": attr.string(mandatory = True),
        "embed_dir": attr.string(mandatory = True),
        "package_name": attr.string(mandatory = True),
        "_helper": attr.label(
            default = "//tools/release/wails/helpers:helpers",
            executable = True,
            cfg = "exec",
        ),
    },
)

def wails_asset_library(
        name,
        src,
        importpath,
        package_name = "assets",
        variable_name = "FrontendDist",
        out = "frontend_dist",
        visibility = None,
        **kwargs):
    """Generates a Go library that embeds staged Wails frontend assets."""
    _stage_assets(
        name = name + "_stage",
        src = src,
        out = out,
    )

    _generate_assets_file(
        name = name + "_go",
        variable_name = variable_name,
        embed_dir = out,
        package_name = package_name,
    )

    go_library(
        name = name,
        srcs = [name + "_go"],
        embedsrcs = [name + "_stage"],
        importpath = importpath,
        visibility = visibility,
        **kwargs
    )

def wails_asset_provider(
        name,
        library,
        importpath,
        visibility = None,
        **kwargs):
    """Provides WailsAssetsInfo for a Go assets library."""
    _wails_assets_provider(
        name = name,
        library = library,
        importpath = importpath,
        visibility = visibility,
        **kwargs
    )
