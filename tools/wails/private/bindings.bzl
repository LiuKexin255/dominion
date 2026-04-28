"""Wails bindings generation rule."""

load("@rules_go//go:def.bzl", "go_context")

_HERMETIC_ENV = {
    "HOME": "/tmp/wails_action_home",
    "TMPDIR": "/tmp",
    "NO_COLOR": "1",
}

def _wails_bindings_impl(ctx):
    wails_toolchain = ctx.toolchains["//tools/wails:toolchain_type"].wails_toolchain
    wails = wails_toolchain.wails
    go = go_context(ctx)
    go_executable = go.sdk.go
    wails_json = ctx.file.wails_json

    go_srcs = depset(transitive = [src.files for src in ctx.attr.go_srcs]).to_list()
    output = ctx.actions.declare_directory(ctx.label.name + "_wailsjs")

    # The wailsjsdir must point to the output directory so bindings are
    # written into the declared output. We inject it via sed in the launcher
    # script — no python3 dependency needed.
    out_path_for_wailsjsdir = output.path

    launcher = ctx.actions.declare_file(ctx.label.name + "_generate.sh")

    # Build the launcher script content. We use string replacement
    # (not .format) to avoid issues with bash braces conflicting
    # with Python format string syntax.
    launcher_content = """#!/usr/bin/env bash
set -euo pipefail

WAILS="$1"
GO="$2"
WAILS_JSON="$3"
OUT="$4"
TAGS="$5"
WAILSJSDIR="__WAILSJS_DIR__"
shift 5

case "$WAILS" in
  /*) ;;
  *) WAILS="$PWD/$WAILS" ;;
esac
case "$GO" in
  /*) ;;
  *) GO="$PWD/$GO" ;;
esac
if [[ -n "${GOROOT:-}" ]]; then
  case "$GOROOT" in
    /*) ;;
    *) export GOROOT="$PWD/$GOROOT" ;;
  esac
fi
case "$OUT" in
  /*) ;;
  *) OUT="$PWD/$OUT" ;;
esac
export GOFLAGS="${GOFLAGS:-} -mod=mod"
export GOPROXY=off

rm -rf "$OUT"
mkdir -p "$OUT"
export GOPATH="$OUT/_gopath"
export GOMODCACHE="$OUT/_gomodcache"
export GOCACHE="$OUT/_gocache"

WORK="$OUT/_wails_project"
mkdir -p "$WORK"
cleanup() {
  rm -rf "$WORK" "$GOPATH" "$GOMODCACHE" "$GOCACHE"
}
trap cleanup EXIT

for src in "$@"; do
  cp "$src" "$WORK/$(basename "$src")"
done

# Rewrite wails.json: inject wailsjsdir pointing to the output directory.
# wails.json fixtures don't have this key, so we prepend it after the
# opening brace. Use sed with | delimiter to avoid path conflicts.
cp "$WAILS_JSON" "$WORK/wails.json"
sed -i '1s|{|{"wailsjsdir": "'"$WAILSJSDIR"'",|' "$WORK/wails.json"

cmd=("$WAILS" generate module -compiler "$GO" -v 2)
if [[ -n "$TAGS" ]]; then
  cmd+=(-tags "$TAGS")
fi

(cd "$WORK" && "${cmd[@]}")
""".replace("__WAILSJS_DIR__", out_path_for_wailsjsdir)

    ctx.actions.write(
        output = launcher,
        content = launcher_content,
        is_executable = True,
    )

    args = ctx.actions.args()
    args.add(wails)
    args.add(go_executable)
    args.add(wails_json)
    args.add(output.path)
    args.add(",".join(ctx.attr.build_tags))
    args.add_all(go_srcs)

    tool_files = [wails, go_executable, launcher]
    if wails_toolchain.runfiles:
        tool_files.extend(wails_toolchain.runfiles.files.to_list())
    tool_files.extend(go.sdk.tools.to_list())
    tool_files.extend(go.sdk.headers.to_list())
    tool_files.extend(go.sdk.srcs.to_list())

    ctx.actions.run(
        inputs = go_srcs + [wails_json],
        outputs = [output],
        executable = launcher,
        arguments = [args],
        env = dict(go.env.items() + _HERMETIC_ENV.items()),
        tools = tool_files,
        mnemonic = "WailsBindings",
        progress_message = "Generating Wails bindings: " + output.basename,
    )

    return [DefaultInfo(files = depset([output]))]

_wails_bindings = rule(
    implementation = _wails_bindings_impl,
    attrs = {
        "go_srcs": attr.label_list(
            doc = "Go source files or source targets containing bound structs.",
            allow_files = True,
            mandatory = True,
        ),
        "wails_json": attr.label(
            doc = "The wails.json project config file.",
            allow_single_file = True,
            mandatory = True,
        ),
        "build_tags": attr.string_list(
            doc = "Build tags passed to Wails bindings generation.",
        ),
    },
    toolchains = [
        "//tools/wails:toolchain_type",
        "@rules_go//go:toolchain",
    ],
    doc = "Generates TypeScript/JavaScript Wails bindings from Go source files.",
)

def wails_bindings(name, go_srcs, wails_json, tags = [], **kwargs):
    """Generates Wails JS/TS bindings into a Bazel declared directory.

    Args:
        name: Target name.
        go_srcs: Go source files or source targets that influence bindings.
        wails_json: Label of the Wails project config file.
        tags: Build tags passed to Wails bindings generation.
        **kwargs: Additional keyword arguments forwarded to the underlying rule.
    """
    _wails_bindings(
        name = name,
        build_tags = tags,
        go_srcs = go_srcs,
        wails_json = wails_json,
        **kwargs
    )
