package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSHA256File(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "empty", content: "", want: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{name: "hello", content: "hello", want: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "artifact.bin")
			if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
				t.Fatalf("WriteFile(%q) unexpected error: %v", path, err)
			}

			got, err := SHA256File(path)

			if err != nil {
				t.Fatalf("SHA256File(%q) unexpected error: %v", path, err)
			}
			if got != tt.want {
				t.Fatalf("SHA256File(%q) = %q, want %q", path, got, tt.want)
			}
		})
	}
}

func TestSHA256Sums(t *testing.T) {
	artifacts := []*Artifact{
		{Filename: "a.zip", SHA256: "aaa"},
		{Filename: "input-helper.exe", SHA256: "bbb"},
	}

	got := SHA256Sums(artifacts)
	want := "aaa  a.zip\nbbb  input-helper.exe\n"

	if got != want {
		t.Fatalf("SHA256Sums() = %q, want %q", got, want)
	}
}
