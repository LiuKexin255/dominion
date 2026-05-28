"""Generic Vite build rule for Bazel.

Produces a tree artifact (directory) containing the Vite build output.
Uses declare_directory + run_shell so that downstream rules can consume
the entire dist directory as a single artifact.

Key design decisions:
  - Resolves the frontend package directory from the `package_json` label
    (not from srcs ordering), making the rule robust and self-documenting.
  - Writes directly to the declared output directory via `--outDir`,
    avoiding a source-tree copy.
  - Runs locally (execution_requirements={"local": ""}) because Vite needs
    access to the source-tree node_modules.
"""

def _vite_build_impl(ctx):
    dist_out = ctx.actions.declare_directory(ctx.attr.out)

    package_json = ctx.file.package_json

    # Collect all file inputs so Bazel tracks dependencies correctly.
    all_inputs = list(ctx.files.srcs) + [package_json, ctx.file.index_html, ctx.file.config]
    if ctx.file.tsconfig:
        all_inputs.append(ctx.file.tsconfig)
    if ctx.file.svelte_config:
        all_inputs.append(ctx.file.svelte_config)

    ctx.actions.run_shell(
        outputs = [dist_out],
        inputs = all_inputs,
        command = """set -euo pipefail

# $1 = resolved path to package.json (symlink resolved by Bazel)
# $2 = the declared output directory path (relative to execroot)

# Absolutize the output path before changing directory
OUTPUT_DIR="$2"
case "$OUTPUT_DIR" in
    /*) ;;
    *) OUTPUT_DIR="$PWD/$OUTPUT_DIR" ;;
esac

# Locate the frontend package directory from the package_json label.
FRONTEND_DIR=$(dirname $(readlink -f "$1"))
cd "$FRONTEND_DIR"

# Build directly into the declared output directory.
./node_modules/.bin/vite build --outDir "$OUTPUT_DIR" --emptyOutDir
""",
        arguments = [
            package_json.path,
            dist_out.path,
        ],
        mnemonic = "ViteBuild",
        progress_message = "Building frontend with vite %{label}",
        execution_requirements = {"local": ""},
    )

    return [DefaultInfo(files = depset([dist_out]))]

vite_build = rule(
    implementation = _vite_build_impl,
    doc = """Runs `vite build` and produces a tree artifact directory.

The rule locates the frontend package directory by resolving the `package_json`
label, then runs Vite with `--outDir` pointing at the declared Bazel output
directory so no intermediate copy is needed.

Example usage:
    load("//tools/dev/js:vite.bzl", "vite_build")

    vite_build(
        name = "build",
        package_json = "package.json",
        index_html = "index.html",
        config = "vite.config.ts",
        tsconfig = "tsconfig.json",
        svelte_config = "svelte.config.js",
        srcs = glob(["src/**"]),
        out = "dist",
        visibility = ["//visibility:public"],
    )
""",
    attrs = {
        "package_json": attr.label(
            allow_single_file = True,
            mandatory = True,
            doc = "The package.json file of the frontend package. Used to locate the project root.",
        ),
        "index_html": attr.label(
            allow_single_file = True,
            mandatory = True,
            doc = "The index.html entry point for the Vite project.",
        ),
        "config": attr.label(
            allow_single_file = True,
            mandatory = True,
            doc = "The vite.config file (e.g. vite.config.ts).",
        ),
        "tsconfig": attr.label(
            allow_single_file = True,
            mandatory = False,
            doc = "Optional tsconfig.json for TypeScript projects.",
        ),
        "svelte_config": attr.label(
            allow_single_file = True,
            mandatory = False,
            doc = "Optional svelte.config.js for Svelte projects.",
        ),
        "srcs": attr.label_list(
            allow_files = True,
            default = [],
            doc = "Source files consumed by the Vite build.",
        ),
        "out": attr.string(
            default = "dist",
            doc = "Name of the output directory tree artifact.",
        ),
    },
)
