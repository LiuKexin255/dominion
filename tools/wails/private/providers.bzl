"""Providers for the Wails Bazel toolchain."""

WailsToolchainInfo = provider(
    doc = "Information about the Wails CLI toolchain.",
    fields = {
        "wails": "Wails CLI executable File",
        "version": "Pinned Wails CLI version string",
        "runfiles": "Runfiles required by Wails CLI (depset)",
    },
)

WailsAppInfo = provider(
    doc = "Information about a Wails application build.",
    fields = {
        "binary": "Produced application binary (File)",
        "frontend_assets": "Declared frontend assets used by embed (directory)",
        "bindings": "Declared bindings output, if generated (directory)",
        "resources": "Platform resources, if generated (File)",
    },
)
