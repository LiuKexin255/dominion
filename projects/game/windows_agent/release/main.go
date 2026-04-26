package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	version := flag.String("version", "", "Agent version (required)")
	s3URL := flag.String("s3-url", "", "S3 URL in format s3://bucket/prefix (required)")
	distDir := flag.String("dist-dir", "", "Distribution directory containing artifacts (required)")
	flag.Parse()

	if *version == "" || *s3URL == "" || *distDir == "" {
		fmt.Fprintf(os.Stderr, "Usage: publish_s3 --version=<ver> --s3-url=<s3://bucket/prefix> --dist-dir=<dir>\n")
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		flag.CommandLine.PrintDefaults()
		os.Exit(1)
	}

	if err := publish(*version, *s3URL, *distDir); err != nil {
		fmt.Fprintf(os.Stderr, "publish failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("publish completed successfully")
}
