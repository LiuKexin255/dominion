// Package log provides a structured logger that emits log entries to the
// Wails frontend log panel. It replaces the standard library log package for
// in-app logging so that all diagnostic output appears in the desktop window
// instead of the console.
//
// Initialize the global logger early in the application lifecycle (typically
// during WailsInit) by calling SetGlobal. Packages that do not own the Wails
// context should use the package-level convenience functions (Info, Error,
// Warn, Printf, Errorf) which delegate to the global logger.
package log
