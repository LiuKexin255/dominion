"""Public API for Wails Bazel rules.

Re-exports all public symbols from private/ modules.
"""

load("//tools/release/wails/private:providers.bzl", _WailsAssetsInfo = "WailsAssetsInfo", _WailsAppInfo = "WailsAppInfo")
load("//tools/release/wails/private:assets.bzl", _wails_asset_library = "wails_asset_library")
load("//tools/release/wails/private:go_binary.bzl", _wails_go_binary = "wails_go_binary")
load("//tools/release/wails/private:app.bzl", _wails_app = "wails_app")
load("//tools/release/wails/private:windows_resources.bzl", _wails_windows_resources = "wails_windows_resources")

WailsAssetsInfo = _WailsAssetsInfo
WailsAppInfo = _WailsAppInfo
wails_asset_library = _wails_asset_library
wails_go_binary = _wails_go_binary
wails_app = _wails_app
wails_windows_resources = _wails_windows_resources
