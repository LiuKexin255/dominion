package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

type windowsInfo struct {
	CompanyName      string `json:"company_name"`
	ProductName      string `json:"product_name"`
	ProductVersion   string `json:"product_version"`
	FileDescription  string `json:"file_description"`
	Copyright        string `json:"copyright"`
	Comments         string `json:"comments"`
	InternalName     string `json:"internal_name"`
	OriginalFilename string `json:"original_filename"`
}

func main() {
	if err := generate(); err != nil {
		fmt.Fprintf(os.Stderr, "generate_winres: %v\n", err)
		os.Exit(1)
	}
}

func generate() error {
	iconPath := flag.String("icon", "", "Path to .ico file")
	manifestPath := flag.String("manifest", "", "Path to .manifest file")
	infoPath := flag.String("info", "", "Path to info.json")
	outPath := flag.String("out", "", "Output .syso path")
	archStr := flag.String("arch", "amd64", "Target architecture (amd64, 386, arm64)")
	flag.Parse()

	if *iconPath == "" || *outPath == "" {
		return fmt.Errorf("usage: generate_winres -icon <ico> -out <syso> [-manifest <manifest>] [-info <info.json>]")
	}

	rs := winres.ResourceSet{}
	if err := addIcon(&rs, *iconPath); err != nil {
		return err
	}
	if *infoPath != "" {
		if err := addVersionInfo(&rs, *infoPath); err != nil {
			return err
		}
	}
	if *manifestPath != "" {
		if err := addManifest(&rs, *manifestPath); err != nil {
			return err
		}
	}

	outFile, err := os.Create(*outPath)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	defer outFile.Close()

	arch, err := archFromString(*archStr)
	if err != nil {
		return err
	}
	if err := rs.WriteObject(outFile, arch); err != nil {
		return fmt.Errorf("write .syso: %w", err)
	}
	return nil
}

func addIcon(rs *winres.ResourceSet, iconPath string) error {
	iconData, err := os.ReadFile(iconPath)
	if err != nil {
		return fmt.Errorf("read icon: %w", err)
	}

	icon, err := winres.LoadICO(bytes.NewReader(iconData))
	if err != nil {
		return fmt.Errorf("parse icon: %w", err)
	}
	if err := rs.SetIcon(winres.ID(1), icon); err != nil {
		return fmt.Errorf("set icon: %w", err)
	}
	return nil
}

func addVersionInfo(rs *winres.ResourceSet, infoPath string) error {
	infoData, err := os.ReadFile(infoPath)
	if err != nil {
		return fmt.Errorf("read info.json: %w", err)
	}

	var info windowsInfo
	if err := json.Unmarshal(infoData, &info); err != nil {
		return fmt.Errorf("parse info.json: %w", err)
	}

	versionInfo := version.Info{}
	if info.ProductVersion != "" {
		versionInfo.SetProductVersion(info.ProductVersion)
		versionInfo.SetFileVersion(info.ProductVersion)
	}
	fields := map[string]string{
		version.CompanyName:      info.CompanyName,
		version.ProductName:      info.ProductName,
		version.ProductVersion:   info.ProductVersion,
		version.FileDescription:  info.FileDescription,
		version.LegalCopyright:   info.Copyright,
		version.Comments:         info.Comments,
		version.InternalName:     info.InternalName,
		version.OriginalFilename: info.OriginalFilename,
	}
	for key, value := range fields {
		if value == "" {
			continue
		}
		if err := versionInfo.Set(version.LangNeutral, key, value); err != nil {
			return fmt.Errorf("set version field %s: %w", key, err)
		}
	}

	rs.SetVersionInfo(versionInfo)
	return nil
}

func archFromString(s string) (winres.Arch, error) {
	switch s {
	case "amd64", "x86_64":
		return winres.ArchAMD64, nil
	case "386", "i386", "x86":
		return winres.ArchI386, nil
	case "arm64", "aarch64":
		return winres.ArchARM64, nil
	default:
		return "", fmt.Errorf("unsupported architecture: %s", s)
	}
}

func addManifest(rs *winres.ResourceSet, manifestPath string) error {
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	if err := rs.Set(winres.RT_MANIFEST, winres.ID(1), winres.LCIDDefault, manifestData); err != nil {
		return fmt.Errorf("set manifest: %w", err)
	}
	return nil
}
