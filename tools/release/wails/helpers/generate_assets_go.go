package main

import (
	"fmt"
	"os"
	"path/filepath"
	"text/template"
)

const assetsGoTemplate = `package {{.PackageName}}

import "embed"

//go:embed all:{{.EmbedDir}}
var {{.VarName}} embed.FS
`

type assetsGoParams struct {
	PackageName string
	EmbedDir    string
	VarName     string
}

// generateAssetsGo creates a Go source file with an embed directive.
func generateAssetsGo(varName string, embedDir string, packageName string, outputFile string) error {
	if varName == "" {
		return fmt.Errorf("variable name must not be empty")
	}
	if embedDir == "" {
		return fmt.Errorf("embed directory must not be empty")
	}
	if packageName == "" {
		return fmt.Errorf("package name must not be empty")
	}

	tmpl, err := template.New("assets").Parse(assetsGoTemplate)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(outputFile), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	f, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer f.Close()

	params := assetsGoParams{
		PackageName: packageName,
		EmbedDir:    embedDir,
		VarName:     varName,
	}

	if err := tmpl.Execute(f, params); err != nil {
		return fmt.Errorf("execute template: %w", err)
	}

	return nil
}

func runGenerateAssetsGo() error {
	args := os.Args[2:]
	if len(args) < 4 {
		return fmt.Errorf("usage: helpers generate_assets_go <variable_name> <embed_dir> <output_file> <package_name>")
	}

	return generateAssetsGo(args[0], args[1], args[3], args[2])
}
