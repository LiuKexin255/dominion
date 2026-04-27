"""Wails frontend assets rule — re-roots frontend build output for Go embed."""

def _wails_frontend_assets_impl(ctx):
    src_files = ctx.attr.src[DefaultInfo].files.to_list()
    if not src_files:
        fail("wails_frontend_assets: src tree artifact is empty")

    out_name = ctx.attr.out if ctx.attr.out else ctx.label.name
    output = ctx.actions.declare_directory(out_name)

    # The frontend dist target is a tree artifact; copy its root contents into
    # the re-rooted output directory so Go embed sees frontend_dist/index.html.
    src_dir = src_files[0].path

    args = ctx.actions.args()
    args.add(src_dir)
    args.add(output.path)

    ctx.actions.run_shell(
        inputs = src_files,
        outputs = [output],
        arguments = [args],
        command = 'cp -r "$1"/* "$2"/',
        mnemonic = "WailsFrontendAssets",
        progress_message = "Copying frontend assets into embed directory: " + output.basename,
    )

    return [DefaultInfo(files = depset([output]))]

_wails_frontend_assets = rule(
    implementation = _wails_frontend_assets_impl,
    attrs = {
        "src": attr.label(
            cfg = "exec",
            doc = "Bazel target producing the frontend build tree artifact",
            mandatory = True,
        ),
        "out": attr.string(
            doc = "Output directory name (defaults to target name)",
        ),
    },
)

def wails_frontend_assets(name, src, out = "", **kwargs):
    """Copies a frontend tree artifact into a re-rooted output directory for Go embed.

    Args:
        name: Target name.
        src: Label of the input tree artifact (e.g. //my/project/frontend:dist).
        out: Output directory name (defaults to target name).
        **kwargs: Additional args passed to the rule.
    """
    _wails_frontend_assets(
        name = name,
        src = src,
        out = out,
        **kwargs
    )
