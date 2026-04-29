package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) == 0 {
		fmt.Fprintln(os.Stderr, "error: no arguments")
		os.Exit(1)
	}

	// Dispatch based on the first positional argument (subcommand).
	// When invoked as separate Bazel actions, the calling rule passes
	// the subcommand name as the first argument.
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "error: missing subcommand (stage_frontend|generate_assets_go|generate_winres)")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "stage_frontend":
		if err := runStageFrontend(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "generate_assets_go":
		if err := runGenerateAssetsGo(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "generate_winres":
		if err := runGenerateWinres(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "error: unknown subcommand %q\n", os.Args[1])
		os.Exit(1)
	}
}
