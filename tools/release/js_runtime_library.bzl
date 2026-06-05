"""Helper rule for exposing JsRuntimePackageInfo on workspace package targets.

Wraps a compiled :lib target (ts_project output) and its package.json to
provide both DefaultInfo (for npm_link_all_packages workspace:* resolution)
and JsRuntimePackageInfo (for artifact_pkg_js runtime_deps closure walking).
"""

load("//tools/release:defs.bzl", "JsRuntimePackageInfo")

def _js_runtime_pkg_impl(ctx):
    lib_files = ctx.attr.lib.files

    return [
        DefaultInfo(files = lib_files),
        JsRuntimePackageInfo(
            package_name = ctx.attr.package_name,
            package_metadata = ctx.file.package_json,
            runtime_files = lib_files,
            type_files = depset([f for f in lib_files.to_list() if f.extension == "d.ts"]),
            runtime_deps = ctx.attr.runtime_deps,
            npm_deps = ctx.attr.npm_deps,
        ),
    ]

js_runtime_library = rule(
    implementation = _js_runtime_pkg_impl,
    doc = """Creates a workspace package target that exposes JsRuntimePackageInfo.

    Replaces plain ``js_library`` ``:pkg`` targets in shared workspace packages
    so that ``artifact_pkg_js`` can discover them via ``runtime_deps`` and walk
    their transitive closure automatically.

    Also provides ``DefaultInfo`` with the lib files so that
    ``npm_link_all_packages`` can resolve ``workspace:*`` dependencies.
    """,
    attrs = {
        "lib": attr.label(
            mandatory = True,
            doc = "Compiled library target (e.g., :lib ts_project).",
        ),
        "package_name": attr.string(
            mandatory = True,
            doc = "Canonical npm package name, e.g. @dominion/common-js-logs.",
        ),
        "package_json": attr.label(
            allow_single_file = True,
            mandatory = True,
            doc = "The package.json file for this workspace package.",
        ),
        "runtime_deps": attr.label_list(
            default = [],
            providers = [JsRuntimePackageInfo],
            doc = "Workspace packages this package depends on (targets exposing JsRuntimePackageInfo).",
        ),
        "npm_deps": attr.label_list(
            default = [],
            doc = "npm link targets for third-party packages needed at runtime.",
        ),
    },
)
