package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

// archMapping converts architecture string identifiers to winres.Arch values.
var archMapping = map[string]winres.Arch{
	"amd64": winres.ArchAMD64,
	"arm64": winres.ArchARM64,
	"386":   winres.ArchI386,
	"arm":   winres.ArchARM,
}

// versionInfo represents the JSON structure for Windows version information.
// Fields map to the standard VS_VERSIONINFO string table entries.
type versionInfo struct {
	FileVersion     string `json:"FileVersion"`
	ProductVersion  string `json:"ProductVersion"`
	CompanyName     string `json:"CompanyName"`
	FileDescription string `json:"FileDescription"`
	ProductName     string `json:"ProductName"`
	LegalCopyright  string `json:"LegalCopyright"`
}

// generateWinres creates a Windows .syso resource file from the provided inputs.
// Any combination of icon, manifest, and info may be empty, in which case that
// resource is omitted. At minimum, an empty but valid .syso is always produced.
func generateWinres(iconPath string, manifestPath string, infoPath string, arch string, outputPath string) error {
	rs := winres.ResourceSet{}

	if iconPath != "" {
		if err := setIcon(&rs, iconPath); err != nil {
			return fmt.Errorf("set icon: %w", err)
		}
	}

	if manifestPath != "" {
		if err := setManifest(&rs, manifestPath); err != nil {
			return fmt.Errorf("set manifest: %w", err)
		}
	}

	if infoPath != "" {
		if err := setVersionInfo(&rs, infoPath); err != nil {
			return fmt.Errorf("set version info: %w", err)
		}
	}

	targetArch, ok := archMapping[arch]
	if !ok {
		return fmt.Errorf("unsupported architecture %q (supported: amd64, arm64, 386, arm)", arch)
	}

	if err := os.MkdirAll(outputPath[:len(outputPath)-len(lastPathComponent(outputPath))], 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	out, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer out.Close()

	if err := rs.WriteObject(out, targetArch); err != nil {
		return fmt.Errorf("write object: %w", err)
	}

	return nil
}

// setIcon loads an .ico file and adds it to the resource set.
func setIcon(rs *winres.ResourceSet, iconPath string) error {
	iconFile, err := os.Open(iconPath)
	if err != nil {
		return fmt.Errorf("open icon file: %w", err)
	}
	defer iconFile.Close()

	ico, err := winres.LoadICO(iconFile)
	if err != nil {
		return fmt.Errorf("load icon: %w", err)
	}

	return rs.SetIcon(winres.RT_ICON, ico)
}

// setManifest reads a manifest XML file and adds it to the resource set.
func setManifest(rs *winres.ResourceSet, manifestPath string) error {
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest file: %w", err)
	}

	xmlData, err := winres.AppManifestFromXML(manifestData)
	if err != nil {
		return fmt.Errorf("parse manifest XML: %w", err)
	}

	rs.SetManifest(xmlData)
	return nil
}

// setVersionInfo reads a version info JSON file and adds it to the resource set.
// The JSON format matches the wails info.json structure with fields like
// FileVersion, ProductVersion, CompanyName, etc.
func setVersionInfo(rs *winres.ResourceSet, infoPath string) error {
	infoData, err := os.ReadFile(infoPath)
	if err != nil {
		return fmt.Errorf("read info file: %w", err)
	}

	if len(infoData) == 0 {
		return nil
	}

	// First try the native version.Info JSON format (used by go-winres).
	var vi version.Info
	if err := vi.UnmarshalJSON(infoData); err == nil {
		rs.SetVersionInfo(vi)
		return nil
	}

	// Fallback: parse the simpler wails info.json format.
	var info versionInfo
	if err := json.Unmarshal(infoData, &info); err != nil {
		return fmt.Errorf("parse info JSON: %w", err)
	}

	vi = version.Info{}
	if info.FileVersion != "" {
		vi.Set(0, version.FileVersion, info.FileVersion)
	}
	if info.ProductVersion != "" {
		vi.Set(0, version.ProductVersion, info.ProductVersion)
	}
	if info.CompanyName != "" {
		vi.Set(0, version.CompanyName, info.CompanyName)
	}
	if info.FileDescription != "" {
		vi.Set(0, version.FileDescription, info.FileDescription)
	}
	if info.ProductName != "" {
		vi.Set(0, version.ProductName, info.ProductName)
	}
	if info.LegalCopyright != "" {
		vi.Set(0, version.LegalCopyright, info.LegalCopyright)
	}

	rs.SetVersionInfo(vi)
	return nil
}

// lastPathComponent returns the last path component (filename) of a path.
func lastPathComponent(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}

// runGenerateWinres is the CLI entry point for the generate_winres subcommand.
// Args layout: os.Args[2:] = iconPath, manifestPath, infoPath, arch, outputPath
func runGenerateWinres() error {
	args := os.Args[2:]
	if len(args) != 5 {
		return fmt.Errorf("usage: helpers generate_winres <icon> <manifest> <info> <arch> <output>")
	}

	return generateWinres(args[0], args[1], args[2], args[3], args[4])
}
