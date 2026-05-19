"""Module extension to fetch ffmpeg Windows amd64 binary.

Downloads a pre-built ffmpeg release for Windows amd64 from gyan.dev.
"""

load("@bazel_tools//tools/build_defs/repo:http.bzl", "http_archive")

_FFMPEG_BUILD_FILE = """exports_files([
    "bin/ffmpeg.exe",
])
"""

def _ffmpeg_windows_amd64_impl(ctx):
    http_archive(
        name = "ffmpeg_windows_amd64",
        urls = ["https://github.com/GyanD/codexffmpeg/releases/download/8.1.1/ffmpeg-8.1.1-essentials_build.zip"],
        sha256 = "6f58ce889f59c311410f7d2b18895b33c03456463486f3b1ebc93d97a0f54541",
        strip_prefix = "ffmpeg-8.1.1-essentials_build",
        build_file_content = _FFMPEG_BUILD_FILE,
    )

ffmpeg_windows_amd64 = module_extension(
    implementation = _ffmpeg_windows_amd64_impl,
)
