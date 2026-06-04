"""Bazel rules for publishing release artifacts to S3."""

load("@aspect_bazel_lib//lib:expand_template.bzl", "expand_template")
load("@rules_oci//oci:defs.bzl", "oci_image", "oci_push")
load("@rules_proto//proto:defs.bzl", "ProtoInfo")

_PUSH_TOOL = Label("//tools/release/s3push")
_RUNFILES = Label("@bazel_tools//tools/bash/runfiles")

ArtifactPkgInfo = provider(
    doc = "Release artifact package metadata.",
    fields = {
        "app": "Application name.",
        "binary_name": "Binary or script basename for deploy metadata.",
        "entry": "Entrypoint path relative to /dominion/{app}/{service}.",
        "kind": "Package kind: go or js.",
        "service": "Service name.",
        "tar": "Tar layer file.",
    },
)

def _runfile_path(file):
    if file.owner.workspace_name:
        return file.owner.workspace_name + "/" + file.short_path

    return "_main/" + file.short_path

def _s3_artifacts_push_impl(ctx):
    manifest = ctx.file.manifest
    artifact_files = []
    artifact_args = []

    for artifact in ctx.attr.artifacts:
        files = artifact.files.to_list()
        artifact_files.extend(files)

        canonical_label = str(artifact.label).removeprefix("@@")
        for file in files:
            artifact_args.append((canonical_label, _runfile_path(file)))

    push_tool_path = _runfile_path(ctx.executable._push_tool)
    manifest_path = _runfile_path(manifest)

    lines = [
        "#!/usr/bin/env bash",
        "set -euo pipefail",
        "",
        "set +e",
        "f=bazel_tools/tools/bash/runfiles/runfiles.bash",
        "source \"${RUNFILES_DIR:-/dev/null}/$f\" 2>/dev/null || \\",
        "  source \"$(grep -sm1 \"^$f \" \"${RUNFILES_MANIFEST_FILE:-/dev/null}\" | cut -f2- -d' ')\" 2>/dev/null || \\",
        "  source \"$0.runfiles/$f\" 2>/dev/null || \\",
        "  source \"$(grep -sm1 \"^$f \" \"$0.runfiles_manifest\" | cut -f2- -d' ')\" 2>/dev/null || \\",
        "  source \"$(grep -sm1 \"^$f \" \"$0.exe.runfiles_manifest\" | cut -f2- -d' ')\" 2>/dev/null || {",
        "    echo \"ERROR: cannot find $f\" >&2",
        "    exit 1",
        "  }",
        "set -e",
        "",
        "push_tool=\"$(rlocation \"%s\")\"" % push_tool_path,
        "manifest=\"$(rlocation \"%s\")\"" % manifest_path,
        "",
        "args=(\"--manifest=${manifest}\")",
    ]

    for canonical_label, artifact_path in artifact_args:
        lines.extend([
            "artifact=\"$(rlocation \"%s\")\"" % artifact_path,
            "args+=(\"--artifact=%s,${artifact}\")" % canonical_label,
        ])

    lines.extend([
        "",
        "exec \"${push_tool}\" \"${args[@]}\"",
        "",
    ])

    launcher = ctx.actions.declare_file(ctx.attr.name + ".sh")
    ctx.actions.write(
        output = launcher,
        content = "\n".join(lines),
        is_executable = True,
    )

    runfiles = ctx.runfiles(
        files = [manifest] + artifact_files,
    ).merge(ctx.attr._push_tool[DefaultInfo].default_runfiles).merge(
        ctx.attr._runfiles[DefaultInfo].default_runfiles,
    )

    return [DefaultInfo(
        executable = launcher,
        runfiles = runfiles,
    )]

_s3_artifacts_push = rule(
    implementation = _s3_artifacts_push_impl,
    attrs = {
        "manifest": attr.label(
            allow_single_file = [".yaml", ".yml"],
            mandatory = True,
        ),
        "artifacts": attr.label_list(
            allow_files = True,
            mandatory = True,
        ),
        "_push_tool": attr.label(
            default = _PUSH_TOOL,
            executable = True,
            cfg = "exec",
        ),
        "_runfiles": attr.label(
            default = _RUNFILES,
            cfg = "target",
        ),
    },
    executable = True,
)

def s3_artifacts_push(name, manifest, artifacts, visibility = None):
    """Creates an executable target that pushes release artifacts to S3.

    Args:
        name: Target name.
        manifest: Release YAML manifest label.
        artifacts: Labels of artifact files or single-file-producing targets.
        visibility: Optional visibility for the executable target.
    """
    push_kwargs = {}
    if visibility:
        push_kwargs["visibility"] = visibility

    _s3_artifacts_push(
        name = name,
        manifest = manifest,
        artifacts = artifacts,
        **push_kwargs
    )

def _full_entry(pkg):
    return "/dominion/{app}/{service}/{entry}".format(
        app = pkg.app,
        service = pkg.service,
        entry = pkg.entry,
    )

def _repository(app, service):
    return "registry.liukexin.com/" + app + "/" + service

def _full_target_name(name):
    package_path = native.package_name()
    if package_path:
        return "//" + package_path + ":" + name

    return "//:" + name

def _require_pkg(ctx):
    if ArtifactPkgInfo not in ctx.attr.pkg:
        fail("{}: pkg must be an artifact_pkg_go or artifact_pkg_js target".format(ctx.label))

    return ctx.attr.pkg[ArtifactPkgInfo]

def _validate_pkg(ctx, pkg):
    if ctx.attr.app != pkg.app:
        fail("{}: app {} does not match pkg app {}".format(ctx.label, ctx.attr.app, pkg.app))
    if ctx.attr.service != pkg.service:
        fail("{}: service {} does not match pkg service {}".format(ctx.label, ctx.attr.service, pkg.service))

def _artifact_image_metadata_impl(ctx):
    pkg = _require_pkg(ctx)
    _validate_pkg(ctx, pkg)

    output = ctx.actions.declare_file(ctx.attr.name + ".json")
    content = """{{
  "schema_version": "3.0",
  "app": "{app}",
  "service": "{service}",
  "binary": "{binary}",
  "entrypoint": "{entrypoint}",
  "image_target": "{image_target}",
  "push_target": "{push_target}",
  "repository": "{repository}",
  "tag": "latest"
}}
""".format(
        app = pkg.app,
        service = pkg.service,
        binary = pkg.binary_name,
        entrypoint = _full_entry(pkg),
        image_target = ctx.attr.image_target,
        push_target = ctx.attr.push_target,
        repository = _repository(pkg.app, pkg.service),
    )

    ctx.actions.write(
        output = output,
        content = content,
    )

    return [DefaultInfo(files = depset([output]))]

_artifact_image_metadata = rule(
    implementation = _artifact_image_metadata_impl,
    attrs = {
        "app": attr.string(mandatory = True),
        "service": attr.string(mandatory = True),
        "image_target": attr.string(mandatory = True),
        "pkg": attr.label(
            mandatory = True,
            providers = [ArtifactPkgInfo],
        ),
        "push_target": attr.string(mandatory = True),
    },
)

def _artifact_pkg_go_impl(ctx):
    binary = ctx.file.binary
    output = ctx.actions.declare_file(ctx.attr.name + ".tar")
    binary_name = ctx.attr.binary.label.name
    package_dir = "dominion/{app}/{service}/bin".format(
        app = ctx.attr.app,
        service = ctx.attr.service,
    )

    ctx.actions.run_shell(
        inputs = [binary],
        outputs = [output],
        arguments = [output.path, binary.path],
        command = """set -euo pipefail
out="$1"
binary="$2"
layer_dir="$(dirname "${{out}}")/{package_dir}"
mkdir -p "${{layer_dir}}"
cp "${{binary}}" "${{layer_dir}}/{binary_name}"
tar -cf "${{out}}" -C "$(dirname "${{out}}")" dominion
""".format(
            binary_name = binary_name,
            package_dir = package_dir,
        ),
        mnemonic = "ArtifactPkgGo",
    )

    return [
        DefaultInfo(files = depset([output])),
        ArtifactPkgInfo(
            app = ctx.attr.app,
            binary_name = binary_name,
            entry = "bin/" + binary_name,
            kind = "go",
            service = ctx.attr.service,
            tar = output,
        ),
    ]

artifact_pkg_go = rule(
    implementation = _artifact_pkg_go_impl,
    doc = """Packages a Go binary into a tar layer.

    The binary is placed at ``/dominion/{app}/{service}/bin/{binary_name}``
    inside the tar and exposes ArtifactPkgInfo for artifact_image.
    """,
    attrs = {
        "app": attr.string(mandatory = True),
        "binary": attr.label(
            allow_single_file = True,
            mandatory = True,
        ),
        "service": attr.string(mandatory = True),
    },
)

def _artifact_pkg_js_impl(ctx):
    output = ctx.actions.declare_file(ctx.attr.name + ".tar")
    base_dir = "dominion/{app}/{service}".format(
        app = ctx.attr.app,
        service = ctx.attr.service,
    )

    inputs = []
    args = ctx.actions.args()
    args.add(output.path)
    args.add(base_dir)

    # Phase 1: ts_project files
    ts_project_label = ctx.attr.ts_project.label
    pkg_prefix = ts_project_label.package + "/" if ts_project_label.package else ""
    for src in ctx.attr.ts_project.files.to_list():
        short = src.short_path
        dest = short[len(pkg_prefix):] if (pkg_prefix and short.startswith(pkg_prefix)) else short
        inputs.append(src)
        args.add(src.path)
        args.add(dest)

    # Phase 2: proto files
    args.add("--proto-files")
    for target in ctx.attr.runtime_protos:
        proto_info = target[ProtoInfo]
        for src in proto_info.transitive_sources.to_list():
            if src.short_path.startswith("google/protobuf/"):
                continue
            if src.owner.workspace_name:
                canonical = src.short_path[len(src.owner.workspace_name) + 1:]
            else:
                canonical = src.short_path
            inputs.append(src)
            args.add(src.path)
            args.add(canonical)

    # Phase 3: npm_deps
    args.add("--npm-deps")
    for target in ctx.attr.npm_deps:
        for src in target.files.to_list():
            node_modules_index = src.short_path.find("node_modules/")
            if node_modules_index < 0:
                continue

            rel = src.short_path[node_modules_index:]

            # Flatten pnpm virtual store paths:
            #   node_modules/.aspect_rules_js/{name}@{ver}/node_modules/{pkg}/...
            # becomes:
            #   node_modules/{pkg}/...
            # so Node's standard module resolver can find packages.
            vs_prefix = "node_modules/.aspect_rules_js/"
            if rel.startswith(vs_prefix):
                nm2 = rel.find("/node_modules/", len(vs_prefix))
                if nm2 >= 0:
                    rel = "node_modules" + rel[nm2 + len("/node_modules"):]

            inputs.append(src)
            args.add(src.path)
            args.add(rel)

    ctx.actions.run_shell(
        inputs = inputs,
        outputs = [output],
        arguments = [args],
        command = """set -euo pipefail
out="$1"
base_dir="$2"
shift 2
# Phase 1: ts_project files
while (( "$#" )); do
  if [[ "$1" == "--proto-files" ]] || [[ "$1" == "--npm-deps" ]]; then
    break
  fi
  src="$1"
  dest="$2"
  shift 2
  dest_path="$(dirname "${out}")/${base_dir}/${dest}"
  mkdir -p "$(dirname "${dest_path}")"
  cp "${src}" "${dest_path}"
done
# Phase 2: proto files
if [[ "${1:-}" == "--proto-files" ]]; then
  shift
  while (( "$#" )); do
    if [[ "$1" == "--npm-deps" ]]; then
      break
    fi
    src="$1"
    dest="$2"
    shift 2
    dest_path="$(dirname "${out}")/${base_dir}/${dest}"
    mkdir -p "$(dirname "${dest_path}")"
    cp "${src}" "${dest_path}"
  done
fi
# Phase 3: npm_deps
if [[ "${1:-}" == "--npm-deps" ]]; then
  shift
  while (( "$#" )); do
    src="$1"
    dest="$2"
    shift 2
    dest_path="$(dirname "${out}")/${base_dir}/${dest}"
    mkdir -p "$(dirname "${dest_path}")"
    rm -rf "${dest_path}"
    cp -aL "${src}" "${dest_path}"
  done
fi
tar -cf "${out}" -C "$(dirname "${out}")" dominion
""",
        mnemonic = "ArtifactPkgJs",
    )

    return [
        DefaultInfo(files = depset([output])),
        ArtifactPkgInfo(
            app = ctx.attr.app,
            binary_name = ctx.attr.entrypoint.split("/")[-1],
            entry = ctx.attr.entrypoint,
            kind = "js",
            service = ctx.attr.service,
            tar = output,
        ),
    ]

artifact_pkg_js = rule(
    implementation = _artifact_pkg_js_impl,
    doc = """Packages JS service files into a tar layer.

    Source files from ``ts_project`` are placed under
    ``/dominion/{app}/{service}/`` using paths relative to the
    ts_project package directory. Proto files from ``runtime_protos``
    are placed at their canonical import paths under the same root.
    Runtime npm dependencies listed in ``npm_deps`` are copied under
    ``/dominion/{app}/{service}/node_modules`` using their Bazel
    node_modules layout, so Node's normal module resolver can find
    them in deployed images.
    """,
    attrs = {
        "app": attr.string(mandatory = True),
        "entrypoint": attr.string(mandatory = True),
        "ts_project": attr.label(
            mandatory = True,
            doc = "A ts_project target. Its DefaultInfo.files (compiled JS) are collected and placed relative to the package directory.",
        ),
        "runtime_protos": attr.label_list(
            allow_empty = True,
            providers = [ProtoInfo],
            doc = "proto_library targets whose transitive proto sources are collected at canonical import paths.",
        ),
        "npm_deps": attr.label_list(
            allow_files = True,
            doc = "Runtime npm dependency targets generated by npm_link_all_packages, such as :node_modules/@grpc/grpc-js.",
        ),
        "service": attr.string(mandatory = True),
    },
)

def _artifact_pkg_entrypoint_impl(ctx):
    pkg = _require_pkg(ctx)
    output = ctx.actions.declare_file(ctx.attr.name + ".txt")

    if pkg.kind == "go":
        content = _full_entry(pkg)
    elif pkg.kind == "js":
        content = "/nodejs/bin/node"
    else:
        fail("{}: unsupported package kind {}".format(ctx.label, pkg.kind))

    ctx.actions.write(output = output, content = content + "\n")
    return [DefaultInfo(files = depset([output]))]

_artifact_pkg_entrypoint = rule(
    implementation = _artifact_pkg_entrypoint_impl,
    attrs = {
        "pkg": attr.label(
            mandatory = True,
            providers = [ArtifactPkgInfo],
        ),
    },
)

def _artifact_pkg_cmd_impl(ctx):
    pkg = _require_pkg(ctx)
    output = ctx.actions.declare_file(ctx.attr.name + ".txt")

    if pkg.kind == "go":
        content = ""
    elif pkg.kind == "js":
        content = _full_entry(pkg) + "\n"
    else:
        fail("{}: unsupported package kind {}".format(ctx.label, pkg.kind))

    ctx.actions.write(output = output, content = content)
    return [DefaultInfo(files = depset([output]))]

_artifact_pkg_cmd = rule(
    implementation = _artifact_pkg_cmd_impl,
    attrs = {
        "pkg": attr.label(
            mandatory = True,
            providers = [ArtifactPkgInfo],
        ),
    },
)

def _artifact_pkg_repository_impl(ctx):
    pkg = _require_pkg(ctx)
    output = ctx.actions.declare_file(ctx.attr.name + ".txt")
    ctx.actions.write(
        output = output,
        content = _repository(pkg.app, pkg.service) + "\n",
    )
    return [DefaultInfo(files = depset([output]))]

_artifact_pkg_repository = rule(
    implementation = _artifact_pkg_repository_impl,
    attrs = {
        "pkg": attr.label(
            mandatory = True,
            providers = [ArtifactPkgInfo],
        ),
    },
)

def _artifact_image_oci_alias_impl(ctx):
    pkg = _require_pkg(ctx)
    if pkg.kind == "go":
        selected = ctx.attr.go_image
    elif pkg.kind == "js":
        selected = ctx.attr.js_image
    else:
        fail("{}: unsupported package kind {}".format(ctx.label, pkg.kind))

    return [DefaultInfo(
        files = selected[DefaultInfo].files,
        runfiles = selected[DefaultInfo].default_runfiles,
    )]

_artifact_image_oci_alias = rule(
    implementation = _artifact_image_oci_alias_impl,
    attrs = {
        "go_image": attr.label(mandatory = True),
        "js_image": attr.label(mandatory = True),
        "pkg": attr.label(
            mandatory = True,
            providers = [ArtifactPkgInfo],
        ),
    },
)

def _artifact_image_push_impl(ctx):
    pkg = _require_pkg(ctx)
    if pkg.kind == "go":
        image = ctx.file.go_image
        push = ctx.executable.go_push
        push_runfiles = ctx.attr.go_push[DefaultInfo].default_runfiles
    elif pkg.kind == "js":
        image = ctx.file.js_image
        push = ctx.executable.js_push
        push_runfiles = ctx.attr.js_push[DefaultInfo].default_runfiles
    else:
        fail("{}: unsupported package kind {}".format(ctx.label, pkg.kind))

    repository = ctx.file.repository
    executable = ctx.actions.declare_file(ctx.attr.name + ".sh")
    content = """#!/usr/bin/env bash
set -euo pipefail

set +e
f=bazel_tools/tools/bash/runfiles/runfiles.bash
source "${{RUNFILES_DIR:-/dev/null}}/$f" 2>/dev/null || \
  source "$(grep -sm1 "^$f " "${{RUNFILES_MANIFEST_FILE:-/dev/null}}" | cut -f2- -d' ')" 2>/dev/null || \
  source "$0.runfiles/$f" 2>/dev/null || \
  source "$(grep -sm1 "^$f " "$0.runfiles_manifest" | cut -f2- -d' ')" 2>/dev/null || \
  source "$(grep -sm1 "^$f " "$0.exe.runfiles_manifest" | cut -f2- -d' ')" 2>/dev/null || {{
    echo "ERROR: cannot find $f" >&2
    exit 1
  }}
set -e

readonly IMAGE_DIR="$(rlocation "{image}")"
readonly REPOSITORY_FILE="$(rlocation "{repository}")"
readonly FIXED_ARGS=()

exec "$(rlocation "{push}")" "$@"
""".format(
        image = _runfile_path(image),
        push = _runfile_path(push),
        repository = _runfile_path(repository),
    )
    ctx.actions.write(
        output = executable,
        content = content,
        is_executable = True,
    )

    runfiles = ctx.runfiles(files = [image, push, repository])
    runfiles = runfiles.merge(push_runfiles).merge(ctx.attr._runfiles[DefaultInfo].default_runfiles)

    return [DefaultInfo(
        executable = executable,
        runfiles = runfiles,
    )]

_artifact_image_push = rule(
    implementation = _artifact_image_push_impl,
    attrs = {
        "go_image": attr.label(
            allow_single_file = True,
            mandatory = True,
        ),
        "go_push": attr.label(
            cfg = "target",
            executable = True,
            mandatory = True,
        ),
        "js_image": attr.label(
            allow_single_file = True,
            mandatory = True,
        ),
        "js_push": attr.label(
            cfg = "target",
            executable = True,
            mandatory = True,
        ),
        "pkg": attr.label(
            mandatory = True,
            providers = [ArtifactPkgInfo],
        ),
        "repository": attr.label(
            allow_single_file = True,
            mandatory = True,
        ),
        "_runfiles": attr.label(
            default = _RUNFILES,
            cfg = "target",
        ),
    },
    executable = True,
)

def artifact_image(name, app, service, pkg, visibility = None):
    """Creates OCI image, push, tag, and metadata targets for a service.

    Accepts a packaging target (from ``artifact_pkg_go`` or ``artifact_pkg_js``)
    and automatically selects base image, entrypoint, and command based on the
    packaging type.

    Packaging conventions:

    +------------+----------------------------+---------------------------+------------------+
    | type       | base image                 | ENTRYPOINT                | CMD              |
    +============+============================+===========================+==================+
    | go         | @distroless_base           | ["/dominion/{a}/{s}/{e}"] | (none)           |
    +------------+----------------------------+---------------------------+------------------+
    | js         | @distroless_nodejs         | ["/nodejs/bin/node"]      | ["/dominion/{a}/{s}/{e}"] |
    +------------+----------------------------+---------------------------+------------------+

    Where ``{a}`` = app, ``{s}`` = service, ``{e}`` = pkg.entry.

    Args:
        name: Base target name. The metadata target uses this exact name.
        app: Application name.
        service: Service name.
        pkg: Target created by ``artifact_pkg_go`` or ``artifact_pkg_js``.
        visibility: Optional visibility for generated targets.
    """
    full_image_target = _full_target_name(name + "_oci")
    full_push_target = _full_target_name(name + "_push")

    kwargs = {}
    if visibility:
        kwargs["visibility"] = visibility

    _artifact_pkg_entrypoint(
        name = name + "_entrypoint",
        pkg = pkg,
    )

    _artifact_pkg_cmd(
        name = name + "_cmd",
        pkg = pkg,
    )

    _artifact_pkg_repository(
        name = name + "_repository",
        pkg = pkg,
    )

    oci_image(
        name = name + "_go_oci_impl",
        base = "@distroless_base",
        entrypoint = ":" + name + "_entrypoint",
        tars = [pkg],
    )

    oci_image(
        name = name + "_js_oci_impl",
        base = "@distroless_nodejs",
        cmd = ":" + name + "_cmd",
        entrypoint = ":" + name + "_entrypoint",
        tars = [pkg],
    )

    _artifact_image_oci_alias(
        name = name + "_oci",
        go_image = ":" + name + "_go_oci_impl",
        js_image = ":" + name + "_js_oci_impl",
        pkg = pkg,
        **kwargs
    )

    expand_template(
        name = name + "_tags",
        out = name + "_tags.txt",
        template = ["latest"],
        **kwargs
    )

    oci_push(
        name = name + "_go_push_impl",
        image = ":" + name + "_go_oci_impl",
        remote_tags = ":" + name + "_tags",
        repository_file = ":" + name + "_repository",
    )

    oci_push(
        name = name + "_js_push_impl",
        image = ":" + name + "_js_oci_impl",
        remote_tags = ":" + name + "_tags",
        repository_file = ":" + name + "_repository",
    )

    _artifact_image_push(
        name = name + "_push",
        go_image = ":" + name + "_go_oci_impl",
        go_push = ":" + name + "_go_push_impl",
        js_image = ":" + name + "_js_oci_impl",
        js_push = ":" + name + "_js_push_impl",
        pkg = pkg,
        repository = ":" + name + "_repository",
        **kwargs
    )

    _artifact_image_metadata(
        name = name,
        app = app,
        service = service,
        image_target = full_image_target,
        pkg = pkg,
        push_target = full_push_target,
        **kwargs
    )
