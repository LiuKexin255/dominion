package service

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// embeddedFiles is the virtual filesystem of message templates shipped
// inside the binary. The //go:embed directive pulls every file under
// testdata/ at build time, so no external data attribute is needed on
// the deployed image.
//
//go:embed testdata/*
var embeddedFiles embed.FS

// MessageStore holds the message templates loaded from the embedded
// testdata directory, validated and ready to serve.
type MessageStore struct {
	messages []*Message
}

// NewMessageStore loads every JSON/YAML message template embedded under
// testdata/, merges them into one flat slice, sorts them alphabetically
// by Name, and runs startup validation. A non-nil error means startup
// must abort: the shipped templates are invalid or absent.
func NewMessageStore() (*MessageStore, error) {
	messages, err := LoadFromFS(embeddedFiles, "testdata")
	if err != nil {
		return nil, fmt.Errorf("load message store: %w", err)
	}
	return &MessageStore{messages: messages}, nil
}

// Messages returns the loaded, validated templates sorted alphabetically
// by Name. The returned slice shares the store's backing array and must
// not be mutated.
func (s *MessageStore) Messages() []*Message {
	return s.messages
}

// LoadFromFS reads every supported message file (.json/.yaml/.yml) under
// rootDir in fsys, parses each into a Message, merges them into one flat
// slice, sorts the slice alphabetically by Name, then validates the
// result. It is the shared loader used by both the embedded store and
// the tests.
func LoadFromFS(fsys fs.FS, rootDir string) ([]*Message, error) {
	var paths []string
	walkErr := fs.WalkDir(fsys, rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if hasMessageExt(path) {
			paths = append(paths, path)
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk %q: %w", rootDir, walkErr)
	}

	sort.Strings(paths)

	var messages []*Message
	for _, p := range paths {
		data, readErr := fs.ReadFile(fsys, p)
		if readErr != nil {
			return nil, fmt.Errorf("read %q: %w", p, readErr)
		}
		msg, parseErr := parseMessage(data, p)
		if parseErr != nil {
			return nil, fmt.Errorf("parse %q: %w", p, parseErr)
		}
		messages = append(messages, msg)
	}

	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Name < messages[j].Name
	})

	if err := Validate(messages); err != nil {
		return nil, err
	}
	return messages, nil
}

func hasMessageExt(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json", ".yaml", ".yml":
		return true
	}
	return false
}

func parseMessage(data []byte, path string) (*Message, error) {
	msg := new(Message)
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		if err := json.Unmarshal(data, msg); err != nil {
			return nil, fmt.Errorf("unmarshal json: %w", err)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, msg); err != nil {
			return nil, fmt.Errorf("unmarshal yaml: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported extension: %s", path)
	}
	return msg, nil
}
