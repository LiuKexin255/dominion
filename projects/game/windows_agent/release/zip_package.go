package main

import (
	"archive/zip"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

type entry struct {
	dest string
	src  string
}

func main() {
	var (
		output    = flag.String("output", "", "output zip file path")
		entryList entriesFlag
		checksums checksumsFlag
	)
	flag.Var(&entryList, "entry", "zip entry in dest=src format (can be repeated)")
	flag.Var(&checksums, "checksum", "generate SHA256 sidecar: checksum_dest=source_dest (can be repeated)")
	flag.Parse()

	if *output == "" {
		fmt.Fprintln(os.Stderr, "error: -output is required")
		os.Exit(1)
	}

	if len(entryList) == 0 {
		fmt.Fprintln(os.Stderr, "error: at least one -entry is required")
		os.Exit(1)
	}

	seen := make(map[string]struct{})
	for _, e := range entryList {
		if e.dest == "" {
			fmt.Fprintln(os.Stderr, "error: empty destination path")
			os.Exit(1)
		}
		if strings.Contains(e.dest, "..") || strings.HasPrefix(e.dest, "/") || strings.HasPrefix(e.dest, "\\") {
			fmt.Fprintf(os.Stderr, "error: invalid destination path: %q\n", e.dest)
			os.Exit(1)
		}
		if _, ok := seen[e.dest]; ok {
			fmt.Fprintf(os.Stderr, "error: duplicate destination path: %q\n", e.dest)
			os.Exit(1)
		}
		seen[e.dest] = struct{}{}
	}

	sort.Slice(entryList, func(i, j int) bool {
		return entryList[i].dest < entryList[j].dest
	})

	srcMap := make(map[string]string, len(entryList))
	for _, e := range entryList {
		srcMap[e.dest] = e.src
	}

	fixedTime := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

	f, err := os.Create(*output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: create output: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	zw := zip.NewWriter(f)

	for _, e := range entryList {
		info, err := os.Stat(e.src)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: stat %q: %v\n", e.src, err)
			os.Exit(1)
		}
		if info.IsDir() {
			fmt.Fprintf(os.Stderr, "error: source is a directory: %q\n", e.src)
			os.Exit(1)
		}

		hdr := &zip.FileHeader{
			Name:   e.dest,
			Method: zip.Deflate,
		}
		hdr.Modified = fixedTime
		hdr.SetMode(0644)

		w, err := zw.CreateHeader(hdr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: create zip header for %q: %v\n", e.dest, err)
			os.Exit(1)
		}

		src, err := os.Open(e.src)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: open %q: %v\n", e.src, err)
			os.Exit(1)
		}
		if _, err := io.Copy(w, src); err != nil {
			src.Close()
			fmt.Fprintf(os.Stderr, "error: copy %q: %v\n", e.src, err)
			os.Exit(1)
		}
		src.Close()
	}

	for _, c := range checksums {
		srcPath, ok := srcMap[c.srcDest]
		if !ok {
			fmt.Fprintf(os.Stderr, "error: checksum source dest %q not found in entries\n", c.srcDest)
			os.Exit(1)
		}

		hash, err := fileSHA256(srcPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: sha256 %q: %v\n", srcPath, err)
			os.Exit(1)
		}

		content := fmt.Sprintf("%s  %s\n", hash, filepathBase(c.srcDest))

		hdr := &zip.FileHeader{
			Name:   c.checksumDest,
			Method: zip.Deflate,
		}
		hdr.Modified = fixedTime
		hdr.SetMode(0644)

		w, err := zw.CreateHeader(hdr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: create zip header for %q: %v\n", c.checksumDest, err)
			os.Exit(1)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			fmt.Fprintf(os.Stderr, "error: write checksum %q: %v\n", c.checksumDest, err)
			os.Exit(1)
		}
	}

	if err := zw.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "error: close zip writer: %v\n", err)
		os.Exit(1)
	}
	if err := f.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "error: close output file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Created %s\n", *output)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func filepathBase(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	idx := strings.LastIndex(path, "/")
	if idx >= 0 {
		return path[idx+1:]
	}
	return path
}

type entriesFlag []entry

func (e *entriesFlag) String() string {
	return fmt.Sprintf("%v", []entry(*e))
}

func (e *entriesFlag) Set(value string) error {
	parts := strings.SplitN(value, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid entry format %q, expected dest=src", value)
	}
	*e = append(*e, entry{dest: parts[0], src: parts[1]})
	return nil
}

type checksumsFlag []struct {
	checksumDest string
	srcDest      string
}

func (c *checksumsFlag) String() string {
	return fmt.Sprintf("%v", []struct{ checksumDest, srcDest string }(*c))
}

func (c *checksumsFlag) Set(value string) error {
	parts := strings.SplitN(value, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid checksum format %q, expected checksum_dest=source_dest", value)
	}
	*c = append(*c, struct {
		checksumDest string
		srcDest      string
	}{checksumDest: parts[0], srcDest: parts[1]})
	return nil
}
