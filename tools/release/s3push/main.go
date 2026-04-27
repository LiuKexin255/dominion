package main

import (
	"context"
	"fmt"
	"os"
)

func main() {
	config, err := ParseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse args: %v\n", err)
		os.Exit(1)
	}

	data, err := os.ReadFile(config.ManifestPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read manifest %q: %v\n", config.ManifestPath, err)
		os.Exit(1)
	}

	manifest, err := ParseManifest(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse manifest: %v\n", err)
		os.Exit(1)
	}

	var manifestTargets []string
	for _, artifact := range manifest.Artifacts {
		manifestTargets = append(manifestTargets, artifact.Target)
	}
	var bazelTargets []string
	for _, artifact := range config.Artifacts {
		bazelTargets = append(bazelTargets, artifact.Label)
	}
	if err = ValidateTargets(manifestTargets, bazelTargets); err != nil {
		fmt.Fprintf(os.Stderr, "validate targets: %v\n", err)
		os.Exit(1)
	}

	stagingDir, err := os.MkdirTemp("", "s3-release-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create staging: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(stagingDir)

	artifactPaths := make(map[string]string)
	for _, artifact := range config.Artifacts {
		artifactPaths[artifact.Label] = artifact.FilePath
	}

	if err = StageArtifacts(stagingDir, manifest.Artifacts, artifactPaths); err != nil {
		fmt.Fprintf(os.Stderr, "stage artifacts: %v\n", err)
		os.Exit(1)
	}
	if _, err = GenerateOutputManifest(manifest, stagingDir); err != nil {
		fmt.Fprintf(os.Stderr, "generate manifest: %v\n", err)
		os.Exit(1)
	}
	if err = GenerateSHA256SUMS(stagingDir); err != nil {
		fmt.Fprintf(os.Stderr, "generate SHA256SUMS: %v\n", err)
		os.Exit(1)
	}

	uploader, err := NewS3Uploader()
	if err != nil {
		fmt.Fprintf(os.Stderr, "create uploader: %v\n", err)
		os.Exit(1)
	}
	if err = Publish(context.Background(), stagingDir, manifest, uploader); err != nil {
		fmt.Fprintf(os.Stderr, "publish: %v\n", err)
		os.Exit(1)
	}
}
