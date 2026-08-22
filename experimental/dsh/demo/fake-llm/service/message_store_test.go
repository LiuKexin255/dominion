package service

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// TestLoadFromFS verifies the happy path: a directory containing both a
// YAML and a JSON messages file is parsed, merged into one slice, and
// sorted alphabetically by Name regardless of filename order.
func TestLoadFromFS(t *testing.T) {
	// given: two files whose filenames sort after their Names so the
	// alphabetical-by-Name ordering is genuinely exercised. The YAML
	// file carries Name "greeting"; the JSON file carries Name "aaa".
	fsys := fstest.MapFS{
		"testdata/zzz_chat.yaml": &fstest.MapFile{
			Data: []byte(strings.Join([]string{
				"messages:",
				"  - name: greeting",
				"    keywords: [hello]",
				"    text: greeting-text",
				"",
			}, "\n")),
		},
		"testdata/aaa_extra.json": &fstest.MapFile{
			Data: []byte(`{"messages":[{"name":"aaa","keywords":["k1","k2"],"text":"aaa-text"}]}`),
		},
	}

	// when
	got, err := LoadFromFS(fsys, "testdata")

	// then: both formats parse, both messages merge, sorted by Name.
	if err != nil {
		t.Fatalf("LoadFromFS unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("LoadFromFS got %d messages, want 2", len(got))
	}
	if got[0].Name != "aaa" || got[1].Name != "greeting" {
		t.Fatalf("LoadFromFS order = [%q, %q], want [aaa, greeting]", got[0].Name, got[1].Name)
	}
	// Verify parsed values survived the round-trip from both formats.
	if got[0].Text != "aaa-text" || !slices.Contains(got[0].Keywords, "k1") {
		t.Fatalf("LoadFromFS aaa values wrong: %+v", got[0])
	}
	if got[1].Text != "greeting-text" || !slices.Contains(got[1].Keywords, "hello") {
		t.Fatalf("LoadFromFS greeting values wrong: %+v", got[1])
	}
}

// TestLoadFromFS_Failure asserts that every startup-invariant violation
// aborts loading with a descriptive error. Each case isolates one
// failure mode so a regression points at the exact rule.
func TestLoadFromFS_Failure(t *testing.T) {
	tests := []struct {
		name    string
		files   fstest.MapFS
		wantErr string
	}{
		{
			name: "malformed json aborts parse",
			files: fstest.MapFS{
				"testdata/bad.json": &fstest.MapFile{
					Data: []byte(`{"messages":[{"name":`),
				},
			},
			wantErr: "unmarshal json",
		},
		{
			name: "malformed yaml aborts parse",
			files: fstest.MapFS{
				"testdata/bad.yaml": &fstest.MapFile{
					Data: []byte("messages:\n  - name: x\n    keywords: [unclosed\n"),
				},
			},
			wantErr: "unmarshal yaml",
		},
		{
			name: "empty messages list rejected",
			files: fstest.MapFS{
				"testdata/empty.yaml": &fstest.MapFile{
					Data: []byte("messages: []\n"),
				},
			},
			wantErr: "declares no messages",
		},
		{
			name: "file without messages key rejected",
			files: fstest.MapFS{
				"testdata/single.yaml": &fstest.MapFile{
					Data: []byte("name: x\nkeywords: [k]\ntext: t\n"),
				},
			},
			wantErr: "declares no messages",
		},
		{
			name: "duplicate name across files rejected",
			files: fstest.MapFS{
				"testdata/a.yaml": &fstest.MapFile{
					Data: []byte("messages:\n  - name: dup\n    keywords: [k1]\n    text: t\n"),
				},
				"testdata/b.yaml": &fstest.MapFile{
					Data: []byte("messages:\n  - name: dup\n    keywords: [k2]\n    text: t\n"),
				},
			},
			wantErr: "duplicate",
		},
		{
			name: "empty keyword element rejected",
			files: fstest.MapFS{
				"testdata/empty_kw.yaml": &fstest.MapFile{
					Data: []byte("messages:\n  - name: x\n    keywords:\n      - \"\"\n      - hi\n    text: t\n"),
				},
			},
			wantErr: "empty keyword",
		},
		{
			name: "empty text rejected",
			files: fstest.MapFS{
				"testdata/no_text.yaml": &fstest.MapFile{
					Data: []byte("messages:\n  - name: x\n    keywords: [k]\n    text: \"\"\n"),
				},
			},
			wantErr: "empty text",
		},
		{
			name: "negative min_turn rejected",
			files: fstest.MapFS{
				"testdata/neg_turn.yaml": &fstest.MapFile{
					Data: []byte("messages:\n  - name: x\n    keywords: [k]\n    min_turn: -1\n    text: t\n"),
				},
			},
			wantErr: "negative min_turn",
		},
		{
			name: "all templates multi-turn leaves fallback unservable",
			files: fstest.MapFS{
				"testdata/multi_only.yaml": &fstest.MapFile{
					Data: []byte(strings.Join([]string{
						"messages:",
						"  - name: greeting-again",
						"    keywords: [hello]",
						"    history_keywords: [hello]",
						"    min_turn: 2",
						"    text: t",
						"",
					}, "\n")),
				},
			},
			wantErr: "no non-multi-turn message",
		},
		{
			name: "zero message files rejected",
			files: fstest.MapFS{
				"testdata/notes.txt": &fstest.MapFile{
					Data: []byte("no message files here"),
				},
			},
			wantErr: "no messages loaded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			_, err := LoadFromFS(tt.files, "testdata")

			// then
			if err == nil {
				t.Fatalf("LoadFromFS expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("LoadFromFS error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestValidate pins the validation rules in isolation, independent of
// file parsing — including the schema differences from the game
// fake-llm: empty keywords ARE valid (pure fallback template).
func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		msgs    []*Message
		wantErr string
	}{
		{
			name:    "valid keyword template",
			msgs:    []*Message{{Name: "a", Keywords: []string{"x"}, Text: "t"}},
			wantErr: "",
		},
		{
			name:    "empty keywords valid (pure fallback template)",
			msgs:    []*Message{{Name: "a", Keywords: []string{}, Text: "t"}},
			wantErr: "",
		},
		{
			name: "multi-turn fields accepted (reserved for US2)",
			msgs: []*Message{{
				Name:            "a",
				Keywords:        []string{"x"},
				HistoryKeywords: []string{"h"},
				MinTurn:         2,
				Text:            "t",
			}, {
				Name:     "b",
				Keywords: []string{"y"},
				Text:     "t",
			}},
			wantErr: "",
		},
		{
			name:    "zero messages rejected",
			msgs:    nil,
			wantErr: "no messages loaded",
		},
		{
			name:    "empty name rejected",
			msgs:    []*Message{{Name: "", Keywords: []string{"x"}, Text: "t"}},
			wantErr: "empty name",
		},
		{
			name:    "empty text rejected",
			msgs:    []*Message{{Name: "a", Keywords: []string{"x"}, Text: ""}},
			wantErr: "empty text",
		},
		{
			name:    "empty-string keyword element rejected",
			msgs:    []*Message{{Name: "a", Keywords: []string{"", "x"}, Text: "t"}},
			wantErr: "empty keyword",
		},
		{
			name: "empty-string history_keyword element rejected",
			msgs: []*Message{{
				Name:            "a",
				Keywords:        []string{"x"},
				HistoryKeywords: []string{""},
				Text:            "t",
			}, {
				Name:     "b",
				Keywords: []string{"y"},
				Text:     "t",
			}},
			wantErr: "empty history_keyword",
		},
		{
			name:    "negative min_turn rejected",
			msgs:    []*Message{{Name: "a", Keywords: []string{"x"}, MinTurn: -1, Text: "t"}},
			wantErr: "negative min_turn",
		},
		{
			name:    "duplicate name rejected",
			msgs:    []*Message{{Name: "a", Keywords: []string{"x"}, Text: "t"}, {Name: "a", Keywords: []string{"y"}, Text: "t"}},
			wantErr: "duplicate",
		},
		{
			name: "only multi-turn templates rejected (unservable fallback)",
			msgs: []*Message{
				{Name: "a", Keywords: []string{"x"}, MinTurn: 2, Text: "t"},
				{Name: "b", Keywords: []string{"y"}, HistoryKeywords: []string{"h"}, Text: "t"},
			},
			wantErr: "no non-multi-turn message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			err := Validate(tt.msgs)

			// then
			if tt.wantErr == "" && err != nil {
				t.Fatalf("Validate unexpected error: %v", err)
			}
			if tt.wantErr != "" && err == nil {
				t.Fatalf("Validate expected error containing %q, got nil", tt.wantErr)
			}
			if tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestNewMessageStore_LoadsEmbeddedChat loads the real testdata embedded
// into the binary and pins the single-source-of-truth strings the US1
// large-test assertions rely on (specs/047-dsh-chat-demo/contracts/
// fake-llm-templates.md §4): greeting.text for the matched round trip,
// farewell.text for the no-match fallback. If these change, the
// testplan cases (T019) must be updated in lockstep.
func TestNewMessageStore_LoadsEmbeddedChat(t *testing.T) {
	// when
	store, err := NewMessageStore()

	// then
	if err != nil {
		t.Fatalf("NewMessageStore unexpected error: %v", err)
	}

	got := store.Messages()
	if len(got) != 3 {
		t.Fatalf("NewMessageStore loaded %d messages, want 3 (chat-only + farewell + greeting)", len(got))
	}

	// Sorted alphabetically: chat-only < farewell < greeting.
	wantNames := []string{"chat-only", "farewell", "greeting"}
	for i, want := range wantNames {
		if got[i].Name != want {
			t.Fatalf("message[%d] = %q, want %q", i, got[i].Name, want)
		}
	}

	chatOnly := got[0]
	if !slices.Contains(chatOnly.Keywords, "chat") {
		t.Errorf("chat-only keywords missing chat: %v", chatOnly.Keywords)
	}
	if chatOnly.Text != "Sure, let's chat!" {
		t.Errorf("chat-only text = %q, want the chat text", chatOnly.Text)
	}

	farewell := got[1]
	if len(farewell.Keywords) != 0 {
		t.Errorf("farewell keywords = %v, want empty (pure fallback template, contracts/fake-llm-templates.md §3.3)", farewell.Keywords)
	}
	if farewell.Text != "I'm sorry, I didn't catch that." {
		t.Errorf("farewell text = %q, want the fallback text", farewell.Text)
	}

	greeting := got[2]
	if !slices.Contains(greeting.Keywords, "hello") {
		t.Errorf("greeting keywords missing hello: %v", greeting.Keywords)
	}
	if greeting.Text != "Hello! How can I help you today?" {
		t.Errorf("greeting text = %q, want the greeting text", greeting.Text)
	}
}
