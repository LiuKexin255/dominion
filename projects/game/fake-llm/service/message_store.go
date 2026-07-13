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
// rootDir in fsys. Files carrying a top-level `tools:` key are parsed
// as tool configs; every remaining file is parsed as a single Message.
// Messages are merged into one flat slice sorted alphabetically by Name;
// tools are merged into another flat slice sorted alphabetically by
// Name. Messages are then validated (at least one message is required;
// tools are optional). It is the shared loader used by both the embedded
// store and the tests.
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
		fileTools, isToolsFile, parseErr := tryParseToolsFile(data, p)
		if parseErr != nil {
			return nil, nil, fmt.Errorf("parse %q: %w", p, parseErr)
		}
		if isToolsFile {
			tools = append(tools, fileTools...)
			continue
		}
		msg, parseErr := parseMessage(data, p)
		if parseErr != nil {
			return nil, nil, fmt.Errorf("parse %q: %w", p, parseErr)
		}
		messages = append(messages, msg)
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

// toolsFile is the probe shape used to detect whether a given config
// file carries a top-level `tools:` key. Only the Tools field is
// inspected; all other keys are ignored by the decoder so message files
// parse cleanly here too (with a nil/empty Tools slice).
type toolsFile struct {
	Tools []*ToolConfig `json:"tools" yaml:"tools"`
}

// tryParseToolsFile attempts to decode data as a tools config file. The
// bool return is true when data carries a non-empty top-level `tools:`
// section, in which case the parsed ToolConfig slice is returned. When
// the file does not declare tools, the caller falls back to the
// single-Message parsing path. A decode error is returned as-is so the
// loader can surface malformed configs at startup.
func tryParseToolsFile(data []byte, path string) ([]*ToolConfig, bool, error) {
	var tf toolsFile
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		if err := json.Unmarshal(data, &tf); err != nil {
			return nil, false, fmt.Errorf("unmarshal json: %w", err)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &tf); err != nil {
			return nil, false, fmt.Errorf("unmarshal yaml: %w", err)
		}
	default:
		return nil, false, fmt.Errorf("unsupported extension: %s", path)
	}
	if len(tf.Tools) == 0 {
		return nil, false, nil
	}
	return tf.Tools, true, nil
}
