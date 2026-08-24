"""Custom Bazel rule to generate TypeScript type definitions from .proto files.

ts_proto_library wraps the officially recommended proto-loader-gen-types CLI
from @grpc/proto-loader. It produces compile-time .ts type files intended for
services that dynamically load .proto files at runtime with @grpc/grpc-js.

This rule does NOT generate static JavaScript or TypeScript protobuf/gRPC stubs;
it produces type definitions only (interfaces for handlers, clients, messages).

**Critical constraint**: The generation options (longs, enums, defaults, oneofs,
keep_case) MUST match the runtime protoLoader.loadSync() options exactly.
Mismatched options will produce type errors when the generated types are used
with the runtime-loaded package definition.

Generated output layout (in the declared directory):
  {package_path}/
    ServiceName.ts              # Service client and handler interfaces
    MessageName.ts              # Message input interface
    MessageName__Output.ts      # Message output (restricted) interface
  {proto_file}.ts               # Master type (named after .proto file, e.g., greeter.ts)
                                # Exports interface ProtoGrpcType

Relative import specifiers in the generated .ts are emitted with a .js
extension (e.g. from './google/protobuf/Duration.js') via the generator's
native --importFileExtension option: consumers type-check the generated
types under NodeNext, which requires explicit extensions on relative
imports. Package specifiers such as '@grpc/grpc-js' are emitted by the
generator verbatim and are unaffected by that option.

Transitive proto dependencies (e.g., Google API annotations) are resolved
via --includeDirs sourced from ProtoInfo.transitive_proto_path, ensuring
that imports such as `google/api/annotations.proto` resolve correctly.
"""

load("@rules_proto//proto:defs.bzl", "ProtoInfo")

def _ts_proto_library_impl(ctx):
    """Implementation of the ts_proto_library rule.

    Extracts ProtoInfo from the proto_library target, passes direct sources
    and transitive include directories to proto-loader-gen-types, and writes
    generated .ts files to a TreeArtifact output directory.
    """
    proto_info = ctx.attr.proto[ProtoInfo]

    # TreeArtifact output: filenames are determined by proto-loader-gen-types
    # based on the proto package and message/service names, so we must use
    # declare_directory rather than declare_file.
    generated_dir = ctx.actions.declare_directory(ctx.attr.name)

    # Collect all transitive proto files as action inputs so Bazel stages them
    # in the sandbox at their execroot paths.
    all_inputs = depset(transitive = [proto_info.transitive_imports])

    # Build argument list for proto-loader-gen-types.
    # NOTE: proto files (positional args) MUST come before flags/options
    # because proto-loader-gen-types.js uses yargs with strict mode that
    # only recognizes positional arguments before option parsing begins.
    args = ctx.actions.args()

    # Pass direct proto sources as positional arguments FIRST.
    args.add_all(proto_info.direct_sources)

    args.add("--outDir", generated_dir.path)
    args.add("--longs", ctx.attr.longs)
    args.add("--enums", ctx.attr.enums)
    args.add("--grpcLib", ctx.attr.grpc_lib)
    args.add("--importFileExtension", ".js")

    if ctx.attr.defaults:
        args.add("--defaults")
    if ctx.attr.oneofs:
        args.add("--oneofs")
    if ctx.attr.keep_case:
        args.add("--keepCase")

    # Pass proto source roots as --includeDirs so that transitive imports
    # (e.g. google/api/annotations.proto, google/protobuf/timestamp.proto)
    # are resolvable by proto-loader-gen-types.
    for proto_path in proto_info.transitive_proto_path.to_list():
        if proto_path:
            args.add("-I", proto_path)

    ctx.actions.run(
        executable = ctx.executable.tool,
        inputs = all_inputs,
        outputs = [generated_dir],
        arguments = [args],
        mnemonic = "ProtoGenTypes",
        progress_message = "Generating TS types from proto %{label}",
        # js_binary from aspect_rules_js changes cwd to BAZEL_BINDIR. Setting it to "."
        # keeps cwd at the execroot so that proto file paths resolve correctly.
        env = {"BAZEL_BINDIR": "."},
    )

    return [DefaultInfo(files = depset([generated_dir]))]

ts_proto_library = rule(
    implementation = _ts_proto_library_impl,
    doc = """Generates TypeScript type definitions from a proto_library target.

Wraps the officially recommended proto-loader-gen-types CLI from
@grpc/proto-loader. Produces compile-time .ts type files consumable by
ts_project for services that dynamically load .proto files at runtime
with @grpc/grpc-js.

This rule does NOT generate static JavaScript or TypeScript protobuf/gRPC
stubs — the output is type definitions only (TypeScript interfaces for
handlers, clients, and messages).

Generated layout:
  {name}/
    {package_path}/
      ServiceName.ts              # GreeterClient + GreeterHandlers types
      MessageName.ts              # Message input interface
      MessageName__Output.ts      # Message output interface
    {proto_file}.ts               # Master type (named after .proto file, e.g., greeter.ts)
                                  # Exports interface ProtoGrpcType

Relative import specifiers in the generated files are emitted with a .js
extension (native --importFileExtension option) so that NodeNext consumers
type-check without per-target fixups; package specifiers such as
'@grpc/grpc-js' are emitted verbatim and are not modified.

Example usage:
  load("//tools/dev/js:ts_proto_library.bzl", "ts_proto_library")

  ts_proto_library(
      name = "greeter_types",
      proto = ":greeter_proto",
  )

The generated directory can be consumed by ts_project:
  ts_project(
      name = "server",
      srcs = [":greeter_types", "src/server.ts"],
      ...
  )

IMPORTANT: The generation options (longs, enums, defaults, oneofs, keep_case)
MUST match the runtime protoLoader.loadSync() options exactly. With the
defaults (longs="String", enums="String", defaults=True, oneofs=True,
keep_case=False), the corresponding runtime options are:
  protoLoader.loadSync("file.proto", {
      longs: String,
      enums: String,
      defaults: true,
      oneofs: true,
      // keepCase omitted (keep_case=False)
  })
""",
    attrs = {
        "proto": attr.label(
            providers = [ProtoInfo],
            mandatory = True,
            doc = "The proto_library target to generate TypeScript types from. " +
                  "Must be a target that provides ProtoInfo (i.e., defined with " +
                  "proto_library from @rules_proto).",
        ),
        "grpc_lib": attr.string(
            default = "@grpc/grpc-js",
            doc = "Import path for the gRPC library used in generated types. " +
                  "This is the module that generated code imports for gRPC types. " +
                  "Must match the library used at runtime (typically @grpc/grpc-js).",
        ),
        "longs": attr.string(
            default = "String",
            values = ["String", "Number"],
            doc = "How to represent 64-bit integer fields in generated types. " +
                  "Must match the runtime loadSync 'longs' option exactly. " +
                  "Options: 'String' (default), 'Number'.",
        ),
        "enums": attr.string(
            default = "String",
            values = ["String", "number"],
            doc = "How to represent enum values in generated types. " +
                  "Must match the runtime loadSync 'enums' option exactly. " +
                  "Options: 'String' (default), 'number'.",
        ),
        "defaults": attr.bool(
            default = True,
            doc = "Whether to include default values in generated output types. " +
                  "When True, passes --defaults to proto-loader-gen-types. " +
                  "Must match the runtime loadSync 'defaults' option exactly.",
        ),
        "oneofs": attr.bool(
            default = True,
            doc = "Whether to include virtual oneof fields in generated types. " +
                  "When True, passes --oneofs to proto-loader-gen-types. " +
                  "Must match the runtime loadSync 'oneofs' option exactly.",
        ),
        "keep_case": attr.bool(
            default = False,
            doc = "Whether to preserve proto field name casing. " +
                  "When True, passes --keepCase to proto-loader-gen-types " +
                  "and the runtime loadSync MUST also set keepCase: true. " +
                  "Default is False (camelCase conversion is applied).",
        ),
        "tool": attr.label(
            executable = True,
            cfg = "exec",
            doc = "The proto-loader-gen-types binary. Defaults to the global " +
                  "//tools/dev/js:proto_loader_gen_types target. Override this " +
                  "for exceptional projects that need a different version of " +
                  "@grpc/proto-loader (e.g., via proto_loader_gen_types_binary() " +
                  "from @grpc/proto-loader package_json.bzl). The overridden " +
                  "generator MUST be @grpc/proto-loader >= 0.7.14: this rule " +
                  "passes --importFileExtension, introduced in 0.7.14 " +
                  "(https://github.com/grpc/grpc-node/pull/2912); older " +
                  "versions silently ignore the flag and emit extensionless " +
                  "relative imports, which break NodeNext consumer typechecks.",
            default = Label("//tools/dev/js:proto_loader_gen_types"),
        ),
    },
)
