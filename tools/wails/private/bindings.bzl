"""Wails bindings generation rule."""

load("@rules_go//go:def.bzl", "go_context")

def _wails_bindings_impl(ctx):
    wails_toolchain = ctx.toolchains["//tools/wails:toolchain_type"].wails_toolchain
    wails = wails_toolchain.wails
    go = go_context(ctx)
    go_executable = go.sdk.go
    wails_json = ctx.file.wails_json

    go_srcs = depset(transitive = [src.files for src in ctx.attr.go_srcs]).to_list()
    output = ctx.actions.declare_directory(ctx.label.name + "_wailsjs")
    launcher = ctx.actions.declare_file(ctx.label.name + "_generate.sh")

    ctx.actions.write(
        output = launcher,
        content = """#!/usr/bin/env bash
set -euo pipefail

WAILS="$1"
GO="$2"
WAILS_JSON="$3"
OUT="$4"
TAGS="$5"
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

python3 - "$WAILS_JSON" "$WORK/wails.json" "$OUT" <<'PY'
import json
import os
import sys

with open(sys.argv[1], encoding="utf-8") as source:
    config = json.load(source)

config["wailsjsdir"] = os.path.dirname(os.path.abspath(sys.argv[3]))
config.setdefault("frontend:install", "")
config.setdefault("frontend:build", "")

with open(sys.argv[2], "w", encoding="utf-8") as target:
    json.dump(config, target, indent=2)
    target.write("\\n")
PY

cmd=("$WAILS" generate module -compiler "$GO" -v 2)
if [[ -n "$TAGS" ]]; then
  cmd+=(-tags "$TAGS")
fi

(cd "$WORK" && "${cmd[@]}")
""",
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
        env = go.env,
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
