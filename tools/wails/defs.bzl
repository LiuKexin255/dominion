"""Public API for the Wails Bazel toolchain."""

# Diagnostics
load("//tools/wails/private:diagnostics.bzl", _wails_doctor = "wails_doctor", _wails_version_test = "wails_version_test")

# Rules
load("//tools/wails/private:config_check.bzl", _wails_config_check = "wails_config_check")
load("//tools/wails/private:frontend_assets.bzl", _wails_frontend_assets = "wails_frontend_assets")
load("//tools/wails/private:bindings.bzl", _wails_bindings = "wails_bindings")
load("//tools/wails/private:windows_resources.bzl", _wails_windows_resources = "wails_windows_resources")
load("//tools/wails/private:app.bzl", _wails_app = "wails_app")

wails_version_test = _wails_version_test
wails_doctor = _wails_doctor
wails_config_check = _wails_config_check
wails_frontend_assets = _wails_frontend_assets
wails_bindings = _wails_bindings
wails_windows_resources = _wails_windows_resources
wails_app = _wails_app
