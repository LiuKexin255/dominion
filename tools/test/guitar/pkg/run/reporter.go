package run

import (
	"fmt"
	"io"
	"os"
)

const (
	colorGreen  = "\x1b[32m"
	colorRed    = "\x1b[31m"
	colorYellow = "\x1b[33m"
	colorReset  = "\x1b[0m"

	statusSuccess = "success"
	statusFailure = "failure"
	statusRunning = "running"

	termDumb = "dumb"
)

// checkTerminal reports whether the writer is attached to a terminal that
// supports ANSI color. Tests can replace it with a stub. It follows the same
// package-level var injectability pattern as stdout, stderr, and runCommand.
var checkTerminal func(io.Writer) bool = defaultCheckTerminal

// Reporter prints suite execution status to a writer, optionally with ANSI
// color codes when the output is a terminal.
type Reporter struct {
	w        io.Writer
	useColor bool
}

// NewReporter creates a Reporter that writes to w. It auto-detects terminal
// capability: the writer must be a character device AND the TERM environment
// variable must be non-empty and not "dumb".
func NewReporter(w io.Writer) *Reporter {
	return &Reporter{
		w:        w,
		useColor: checkTerminal(w),
	}
}

// SuiteHeader prints the suite header line with run metadata. Output is never
// colored — it is structural text only.
func (r *Reporter) SuiteHeader(name, runID, envName, deployPath string) {
	fmt.Fprintf(r.w, "--- Suite: %s ---\n", name)
	fmt.Fprintf(r.w, "  run=%s env=%s deploy=%s\n", runID, envName, deployPath)
}

// SuiteStatus prints the suite status with color: "success"→green, "failure"→red,
// "running"→yellow. If err is non-nil the error message is appended without
// color codes.
func (r *Reporter) SuiteStatus(name string, status string, err error) {
	s := status
	if r.useColor {
		switch status {
		case statusSuccess:
			s = colorGreen + status + colorReset
		case statusFailure:
			s = colorRed + status + colorReset
		case statusRunning:
			s = colorYellow + status + colorReset
		}
	}
	if err != nil {
		fmt.Fprintf(r.w, "suite %s: %s, error: %v\n", name, s, err)
		return
	}
	fmt.Fprintf(r.w, "suite %s: %s\n", name, s)
}

// Step prints an indented step label. Output is never colored — just
// 2-space indented text.
func (r *Reporter) Step(label string) {
	fmt.Fprintf(r.w, "  %s\n", label)
}

// DeployDiagnostics prints a prominent header line marking the start of the
// environment-state diagnostics block emitted when a suite's deploy step fails
// (contract: specs/032-guitar-deploy-failure-state/contracts/guitar-integration.md).
// Output is never colored — structural text only, like Step and SuiteHeader.
func (r *Reporter) DeployDiagnostics(envName string) {
	fmt.Fprintf(r.w, "  --- 环境状态 (env=%s) ---\n", envName)
}

// SuiteResult records the outcome of a single suite execution, consumed by
// Summary to print an end-of-run recap.
type SuiteResult struct {
	Name   string
	Status string
	Err    error
}

// Summary prints a recap of suite outcomes: a total/passed/failed header line
// followed by one status line per suite. It is emitted once at the end of Run
// so `tail` of the output is enough to see every suite's result.
//
// Empty input produces no output.
func (r *Reporter) Summary(results []*SuiteResult) {
	if len(results) == 0 {
		return
	}

	var passed, failed int
	for _, res := range results {
		if res.Status == statusSuccess {
			passed++
		} else {
			failed++
		}
	}

	fmt.Fprintln(r.w, "--- Summary ---")

	counts := fmt.Sprintf("total: %d, passed: %d, failed: %d", len(results), passed, failed)
	if r.useColor {
		if failed > 0 {
			counts = colorRed + counts + colorReset
		} else {
			counts = colorGreen + counts + colorReset
		}
	}
	fmt.Fprintln(r.w, counts)

	for _, res := range results {
		status := res.Status
		if r.useColor {
			switch res.Status {
			case statusSuccess:
				status = colorGreen + res.Status + colorReset
			case statusFailure:
				status = colorRed + res.Status + colorReset
			}
		}
		if res.Err != nil {
			fmt.Fprintf(r.w, "  %s: %s, error: %v\n", res.Name, status, res.Err)
			continue
		}
		fmt.Fprintf(r.w, "  %s: %s\n", res.Name, status)
	}
}

// defaultCheckTerminal returns true when w is a character device file (i.e. a
// real terminal) and the TERM environment variable is non-empty and not "dumb".
func defaultCheckTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	if fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	term := os.Getenv("TERM")
	return term != "" && term != termDumb
}
