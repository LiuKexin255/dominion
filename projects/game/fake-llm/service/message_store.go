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

// MessageStore holds the message templates and tool configs loaded from
// the embedded testdata directory, validated and ready to serve.
type MessageStore struct {
	messages []*Message
	tools    []*ToolConfig
}

// NewMessageStore loads every JSON/YAML message template and tools
// config embedded under testdata/, merges them into flat slices, sorts
// them alphabetically by Name, and runs startup validation. A non-nil
// error means startup must abort: the shipped templates are invalid or
// absent (at least one message is always required; tools are optional).
func NewMessageStore() (*MessageStore, error) {
	messages, tools, err := LoadFromFS(embeddedFiles, "testdata")
	if err != nil {
		return nil, fmt.Errorf("load message store: %w", err)
	}
	return &MessageStore{messages: messages, tools: tools}, nil
}

// Messages returns the loaded, validated message templates sorted
// alphabetically by Name. The returned slice shares the store's backing
// array and must not be mutated.
func (s *MessageStore) Messages() []*Message {
	return s.messages
}

// Tools returns the loaded, validated tool configs sorted alphabetically
// by Name. The slice is empty (not nil) when no tools: section was
// loaded. The returned slice shares the store's backing array and must
// not be mutated.
func (s *MessageStore) Tools() []*ToolConfig {
	return s.tools
}

// LoadFromFS reads every supported config file (.json/.yaml/.yml) under
// rootDir in fsys. Each file's shape is detected by its top-level keys
// (specs/046-fake-llm-think-chunking/data-model.md §4): a `tools:` key
// marks a tool-config file, a `messages:` key marks a multi-message
// file, and a file with neither key is parsed as a single Message. All
// three shapes are merged into one flat message slice and one flat tool
// slice, each sorted alphabetically by Name. Messages are then validated
// (at least one message is required; tools are optional). It is the
// shared loader used by both the embedded store and the tests.
func LoadFromFS(fsys fs.FS, rootDir string) ([]*Message, []*ToolConfig, error) {
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
		return nil, nil, fmt.Errorf("walk %q: %w", rootDir, walkErr)
	}

	sort.Strings(paths)

	var messages []*Message
	var tools []*ToolConfig
	for _, p := range paths {
		data, readErr := fs.ReadFile(fsys, p)
		if readErr != nil {
			return nil, nil, fmt.Errorf("read %q: %w", p, readErr)
		}
		fileMessages, fileTools, parseErr := detectAndParse(data, p)
		if parseErr != nil {
			return nil, nil, fmt.Errorf("parse %q: %w", p, parseErr)
		}
		messages = append(messages, fileMessages...)
		tools = append(tools, fileTools...)
	}

	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Name < messages[j].Name
	})
	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Name < tools[j].Name
	})

	if err := Validate(messages); err != nil {
		return nil, nil, err
	}
	if err := ValidateTools(tools); err != nil {
		return nil, nil, err
	}
	return messages, tools, nil
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

// fileShapeProbe is the combined top-level-key probe used to detect a
// config file's shape (specs/046-fake-llm-think-chunking/research.md D4):
// a non-empty `tools:` section marks a tool-config file and a non-empty
// `messages:` section marks a multi-message file. A file carrying neither
// key is a single-message file, and a file carrying both is rejected as
// ambiguous (validation rule V6). Only these two fields are inspected;
// all other keys are ignored by the decoder so single-message files
// parse cleanly here too (with nil/empty slices).
type fileShapeProbe struct {
	Tools    []*ToolConfig `json:"tools" yaml:"tools"`
	Messages []*Message    `json:"messages" yaml:"messages"`
}

// detectAndParse decodes data into the message/tool slices its top-level
// keys declare (specs/046-fake-llm-think-chunking/data-model.md §4), with
// detection precedence `tools:` > `messages:` > single-message (FR-013).
// A file declaring both `tools:` and `messages:` is rejected — the only
// ambiguous shape (V6, specs/046-fake-llm-think-chunking/research.md D4).
// A decode error is returned as-is so the loader can surface malformed
// configs at startup.
func detectAndParse(data []byte, path string) ([]*Message, []*ToolConfig, error) {
	var probe fileShapeProbe
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		if err := json.Unmarshal(data, &probe); err != nil {
			return nil, nil, fmt.Errorf("unmarshal json: %w", err)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &probe); err != nil {
			return nil, nil, fmt.Errorf("unmarshal yaml: %w", err)
		}
	default:
		return nil, nil, fmt.Errorf("unsupported extension: %s", path)
	}
	if len(probe.Tools) != 0 && len(probe.Messages) != 0 {
		return nil, nil, fmt.Errorf("file declares both tools: and messages: (ambiguous shape)")
	}
	if len(probe.Tools) != 0 {
		return nil, probe.Tools, nil
	}
	if len(probe.Messages) != 0 {
		return probe.Messages, nil, nil
	}
	msg, err := parseMessage(data, path)
	if err != nil {
		return nil, nil, err
	}
	return []*Message{msg}, nil, nil
}
