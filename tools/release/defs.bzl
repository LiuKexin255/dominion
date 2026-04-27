"""Bazel rules for publishing release artifacts to S3."""

load("@aspect_bazel_lib//lib:expand_template.bzl", "expand_template")
load("@rules_oci//oci:defs.bzl", "oci_image", "oci_push")

_PUSH_TOOL = Label("//tools/release/s3push")
_RUNFILES = Label("@bazel_tools//tools/bash/runfiles")

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

def _artifact_image_metadata_impl(ctx):
    output = ctx.actions.declare_file(ctx.attr.name + ".json")
    entrypoint = "/dominion/{}/{}/bin/{}".format(
        ctx.attr.app,
        ctx.attr.service,
        ctx.attr.binary,
    )
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
        app = ctx.attr.app,
        service = ctx.attr.service,
        binary = ctx.attr.binary,
        entrypoint = entrypoint,
        image_target = ctx.attr.image_target,
        push_target = ctx.attr.push_target,
        repository = ctx.attr.repository,
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
        "binary": attr.string(mandatory = True),
        "image_target": attr.string(mandatory = True),
        "push_target": attr.string(mandatory = True),
        "repository": attr.string(mandatory = True),
    },
)

def artifact_image(name, app, service, binary, visibility = None):
    """Creates OCI image, push, tag, tar layer, and metadata targets for a service.

    Args:
        name: Base target name. The metadata target uses this exact name.
        app: Application name.
        service: Service name.
        binary: Bazel label for the service binary.
        visibility: Optional visibility for generated targets.
    """
    binary_name = native.package_relative_label(binary).name
    package_path = native.package_name()
    repository = "registry.liukexin.com/" + app + "/" + service
    entrypoint = "/dominion/" + app + "/" + service + "/bin/" + binary_name
    package_dir = "dominion/" + app + "/" + service + "/bin"

    full_image_target = "//" + package_path + ":" + name + "_oci"
    full_push_target = "//" + package_path + ":" + name + "_push"

    kwargs = {}
    if visibility:
        kwargs["visibility"] = visibility

    native.genrule(
        name = name + "_layer",
        outs = [name + "_layer.tar"],
        srcs = [binary],
        cmd = ("BIN=$$(basename $(location {binary})); " +
               "LAYER_DIR=$$(dirname $@)/{package_dir}; " +
               "mkdir -p \"$${{LAYER_DIR}}\"; " +
               "cp $(location {binary}) \"$${{LAYER_DIR}}/$${{BIN}}\"; " +
               "tar -cf \"$@\" -C \"$$(dirname $@)\" dominion").format(
            binary = binary,
            package_dir = package_dir,
        ),
        **kwargs
    )

    oci_image(
        name = name + "_oci",
        base = "@distroless_base",
        entrypoint = [entrypoint],
        tars = [":" + name + "_layer"],
        **kwargs
    )

    expand_template(
        name = name + "_tags",
        out = name + "_tags.txt",
        template = ["latest"],
        **kwargs
    )

    oci_push(
        name = name + "_push",
        image = ":" + name + "_oci",
        remote_tags = ":" + name + "_tags",
        repository = repository,
        **kwargs
    )

    _artifact_image_metadata(
        name = name,
        app = app,
        service = service,
        binary = binary_name,
        image_target = full_image_target,
        push_target = full_push_target,
        repository = repository,
        **kwargs
    )
