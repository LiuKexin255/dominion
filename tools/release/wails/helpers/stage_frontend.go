package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// stageFrontend copies all files from srcDir into outDir,
// preserving the directory structure.
func stageFrontend(srcDir string, outDir string) error {
	info, err := os.Stat(srcDir)
	if err != nil {
		return fmt.Errorf("stat source directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("source path %q is not a directory", srcDir)
	}
	realSrcDir, err := filepath.EvalSymlinks(srcDir)
	if err != nil {
		return fmt.Errorf("resolve source directory: %w", err)
	}
	srcDir = realSrcDir

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	err = filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return fmt.Errorf("relative path: %w", err)
		}

		targetPath := filepath.Join(outDir, relPath)

		if d.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}

		return copyFile(path, targetPath)
	})
	if err != nil {
		return fmt.Errorf("walk source directory: %w", err)
	}

	return nil
}

// copyDirTree copies all files from srcDir into destDir, preserving structure.
// Used internally for deterministic verification.
func copyDirTree(srcDir string, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create dest directory: %w", err)
	}

	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return fmt.Errorf("relative path: %w", err)
		}

		targetPath := filepath.Join(destDir, relPath)

		if d.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}

		return copyFile(path, targetPath)
	})
}

func copyFile(src string, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create destination file: %w", err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("copy file contents: %w", err)
	}

	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat source file: %w", err)
	}

	return os.Chmod(dst, srcInfo.Mode())
}

func runStageFrontend() error {
	args := os.Args[2:]
	if len(args) < 2 {
		return fmt.Errorf("usage: helpers stage_frontend <source_dir> <output_dir>")
	}

	return stageFrontend(args[0], args[1])
}
