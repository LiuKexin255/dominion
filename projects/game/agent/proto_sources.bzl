"""Runfiles helper: exposes a proto_library's transitive .proto sources.

The v1 server loads `game_agent_legacy.proto` with proto-loader at runtime
(the v1 team surface kept out of game.proto; removed in phase 5,
specs/059-agent-v2-team-mode/tasks.md). `lib_test` needs the file plus its
game.proto and google/api import closure in its runfiles — a proto_library's
default files only carry the descriptor set, so this rule materializes
`ProtoInfo.transitive_sources` instead.
"""

load("@rules_proto//proto:defs.bzl", "ProtoInfo")

def _transitive_proto_sources_impl(ctx):
    sources = depset(
        transitive = [proto[ProtoInfo].transitive_sources for proto in ctx.attr.protos],
    )
    return [DefaultInfo(files = sources)]

transitive_proto_sources = rule(
    implementation = _transitive_proto_sources_impl,
    attrs = {
        "protos": attr.label_list(providers = [ProtoInfo]),
    },
)
