"""Test fixture rules for wails_frontend_assets tests."""

def _mock_frontend_dist_impl(ctx):
    output = ctx.actions.declare_directory("frontend_mock_tree")

    args = ctx.actions.args()
    args.add(output.path)
    args.add_all(ctx.files.srcs)

    ctx.actions.run_shell(
        inputs = ctx.files.srcs,
        outputs = [output],
        arguments = [args],
        command = """
out="$1"; shift
for f in "$@"; do
    rel="${f#*frontend_mock/}"
    dir="${rel%/*}"
    if [ "$dir" != "$rel" ]; then
        mkdir -p "$out/$dir"
    fi
    cp "$f" "$out/$rel"
done
""",
    )

    return [DefaultInfo(files = depset([output]))]

mock_frontend_dist = rule(
    implementation = _mock_frontend_dist_impl,
    attrs = {
        "srcs": attr.label_list(
            allow_files = True,
            mandatory = True,
        ),
    },
)
