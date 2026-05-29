"""Provider definitions for Wails Bazel rules."""

WailsAssetsInfo = provider(
    fields = {
        "library": "Go assets library target consumed by the app go_library",
        "importpath": "Go importpath for the generated assets package",
    },
)

WailsAppInfo = provider(
    fields = {
        "binary": "Produced Windows exe File",
        "assets": "WailsAssetsInfo or equivalent assets provider",
        "bindings": "Declared bindings directory, if generated",
        "resources": "Declared .syso resource file, if generated",
        "platform": "Target platform string, e.g. windows/amd64",
    },
)
