load("@rules_proto//proto:defs.bzl", "ProtoInfo")

def _proto_import_root(src):
    if src.short_path.startswith("projects/"):
        return "."

    for marker in ["/google/api/", "/google/protobuf/"]:
        index = src.path.rfind(marker)
        if index != -1:
            return src.path[:index]

    fail("unsupported proto import path: %s" % src.short_path)

def _quote(value):
    return "'" + value.replace("'", "'\\''") + "'"

def _protobuf_ts_codegen_impl(ctx):
    proto = ctx.attr.proto[ProtoInfo]
    sources = proto.transitive_sources.to_list()
    direct_sources = proto.direct_sources

    if len(direct_sources) != 1:
        fail("expected exactly one direct proto source")

    outputs = [ctx.actions.declare_file(out) for out in ctx.attr.outs]
    output_dir = ctx.bin_dir.path + "/" + ctx.label.package

    import_roots = []
    for src in sources:
        root = _proto_import_root(src)
        if root not in import_roots:
            import_roots.append(root)

    command = [
        "set -eu",
        "mkdir -p " + _quote(output_dir),
        " ".join([
            _quote(ctx.executable.protoc.path),
            " ".join(["-I" + _quote(root) for root in import_roots]),
            "--plugin=protoc-gen-ts=" + _quote(ctx.executable.plugin.path),
            "--ts_out=" + _quote(output_dir),
            "--ts_opt=generate_dependencies,force_disable_services,force_exclude_all_options",
            _quote(direct_sources[0].path),
        ]),
    ]

    ctx.actions.run_shell(
        inputs = depset(sources),
        outputs = outputs,
        tools = [
            ctx.executable.plugin,
            ctx.executable.protoc,
        ],
        command = "\n".join(command),
        env = {"BAZEL_BINDIR": ctx.bin_dir.path},
        mnemonic = "ProtobufTsCodegen",
    )

    return DefaultInfo(files = depset(outputs))

protobuf_ts_codegen = rule(
    implementation = _protobuf_ts_codegen_impl,
    attrs = {
        "outs": attr.string_list(mandatory = True),
        "plugin": attr.label(
            cfg = "exec",
            executable = True,
            mandatory = True,
        ),
        "proto": attr.label(
            mandatory = True,
            providers = [ProtoInfo],
        ),
        "protoc": attr.label(
            cfg = "exec",
            executable = True,
            mandatory = True,
        ),
    },
)
