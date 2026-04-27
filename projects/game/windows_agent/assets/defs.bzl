def _frontend_embed_assets_impl(ctx):
    src_files = ctx.attr.src[DefaultInfo].files.to_list()
    if not src_files:
        fail("frontend_embed_assets: src tree artifact is empty")

    output = ctx.actions.declare_directory(ctx.attr.out)

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
        mnemonic = "FrontendEmbedAssets",
        progress_message = "Copying frontend dist into embed directory: " + output.basename,
    )

    return [DefaultInfo(files = depset([output]))]

_frontend_embed_assets = rule(
    implementation = _frontend_embed_assets_impl,
    attrs = {
        "src": attr.label(
            cfg = "exec",
            mandatory = True,
        ),
        "out": attr.string(
            mandatory = True,
        ),
    },
)

def frontend_embed_assets(name, src, out, **kwargs):
    """Copies a frontend tree artifact into a re-rooted output directory for Go embed.

    The rule takes a tree artifact (e.g. Vite build output) and copies its contents
    into a new directory named by `out` so that Go's //go:embed all:<out> resolves
    correctly.

    Args:
        name: Target name.
        src: Label of the input tree artifact (e.g. //projects/game/windows_agent/frontend:dist).
        out: Output directory name (e.g. "frontend_dist").
    """
    visibility = kwargs.pop("visibility", None)

    rule_kwargs = {}
    if visibility:
        rule_kwargs["visibility"] = visibility

    _frontend_embed_assets(
        name = name,
        src = src,
        out = out,
        **rule_kwargs
    )
