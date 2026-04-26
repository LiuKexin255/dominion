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
        urls = ["https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip"],
        sha256 = "8748283d821613d930b0e7be685aaa9df4ca6f0ad4d0c42fd02622b3623463c6",
        strip_prefix = "ffmpeg-8.1-essentials_build",
        build_file_content = _FFMPEG_BUILD_FILE,
    )

ffmpeg_windows_amd64 = module_extension(
    implementation = _ffmpeg_windows_amd64_impl,
)
