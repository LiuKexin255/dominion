"""Bazel rules for publishing release artifacts to S3."""

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
