package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	executor, err := NewWindowsExecutor()
	if err != nil {
		fmt.Fprintf(os.Stderr, "create executor: %v\n", err)
		os.Exit(1)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signals
		executor.ReleaseAll()
		os.Exit(1)
	}()

	if err := RunIPC(os.Stdin, os.Stdout, executor); err != nil {
		executor.ReleaseAll()
		fmt.Fprintf(os.Stderr, "run ipc: %v\n", err)
		os.Exit(1)
	}
	executor.ReleaseAll()
}
