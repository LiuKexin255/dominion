package service

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// TestLoadFromFS verifies the happy path: a directory containing both a
// YAML and a JSON message file is parsed, merged into one slice, and
// sorted alphabetically by Name regardless of filename order.
func TestLoadFromFS(t *testing.T) {
	// given: two files whose filenames sort after their Names so the
	// alphabetical-by-Name ordering is genuinely exercised. The YAML file
	// carries Name "greeting"; the JSON file carries Name "aaa".
	fsys := fstest.MapFS{
		"testdata/zzz_greeting.yaml": &fstest.MapFile{
			Data: []byte(strings.Join([]string{
				"name: greeting",
				"keywords:",
				"  - hello",
				"  - hi",
				"reasoning: greeting-reasoning",
				"text: greeting-text",
				"",
			}, "\n")),
		},
		"testdata/aaa_aaa.json": &fstest.MapFile{
			Data: []byte(`{"name":"aaa","keywords":["k1","k2"],"reasoning":"aaa-reasoning","text":"aaa-text"}`),
		},
	}

	// when
	got, _, err := LoadFromFS(fsys, "testdata")

	// then: both formats parse, two messages merge, sorted by Name.
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
					Data: []byte(`{"name":`),
				},
			},
			wantErr: "unmarshal json",
		},
		{
			name: "malformed yaml aborts parse",
			files: fstest.MapFS{
				"testdata/bad.yaml": &fstest.MapFile{
					Data: []byte("name: x\nkeywords: [unclosed\n"),
				},
			},
			wantErr: "unmarshal yaml",
		},
		{
			name: "empty keywords slice fails validation",
			files: fstest.MapFS{
				"testdata/empty_kw.yaml": &fstest.MapFile{
					Data: []byte("name: x\nkeywords: []\nreasoning: r\ntext: t\n"),
				},
			},
			wantErr: "no keywords",
		},
		{
			name: "missing keywords field fails validation",
			files: fstest.MapFS{
				"testdata/missing_kw.json": &fstest.MapFile{
					Data: []byte(`{"name":"x","reasoning":"r","text":"t"}`),
				},
			},
			wantErr: "no keywords",
		},
		{
			name: "empty-string keyword element fails validation",
			files: fstest.MapFS{
				"testdata/empty_element.yaml": &fstest.MapFile{
					Data: []byte("name: x\nkeywords:\n  - \"\"\n  - hi\nreasoning: r\ntext: t\n"),
				},
			},
			wantErr: "empty keyword",
		},
		{
			name: "duplicate name across files fails validation",
			files: fstest.MapFS{
				"testdata/a.yaml": &fstest.MapFile{
					Data: []byte("name: dup\nkeywords:\n  - k1\nreasoning: r\ntext: t\n"),
				},
				"testdata/b.json": &fstest.MapFile{
					Data: []byte(`{"name":"dup","keywords":["k2"],"reasoning":"r","text":"t"}`),
				},
			},
			wantErr: "duplicate",
		},
		{
			name: "zero messages fails validation",
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
			_, _, err := LoadFromFS(tt.files, "testdata")
			if err == nil {
				t.Fatalf("LoadFromFS expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("LoadFromFS error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestValidate covers the validation rules in isolation, independent of
// file parsing. It pins the exact set of invariants enforced at startup.
func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		msgs    []*Message
		wantErr string
	}{
		{
			name:    "valid single message",
			msgs:    []*Message{{Name: "a", Keywords: []string{"x"}}},
			wantErr: "",
		},
		{
			name:    "valid multiple distinct messages",
			msgs:    []*Message{{Name: "a", Keywords: []string{"x"}}, {Name: "b", Keywords: []string{"y"}}},
			wantErr: "",
		},
		{
			name:    "zero messages rejected",
			msgs:    nil,
			wantErr: "no messages loaded",
		},
		{
			name:    "empty keywords rejected",
			msgs:    []*Message{{Name: "a", Keywords: []string{}}},
			wantErr: "no keywords",
		},
		{
			name:    "nil keywords rejected",
			msgs:    []*Message{{Name: "a"}},
			wantErr: "no keywords",
		},
		{
			name:    "empty-string keyword element rejected",
			msgs:    []*Message{{Name: "a", Keywords: []string{"", "x"}}},
			wantErr: "empty keyword",
		},
		{
			name:    "duplicate name rejected",
			msgs:    []*Message{{Name: "a", Keywords: []string{"x"}}, {Name: "a", Keywords: []string{"y"}}},
			wantErr: "duplicate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.msgs)
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

// TestNewMessageStore_LoadsEmbeddedSamples loads the real testdata
// embedded into the binary and pins the single-source-of-truth strings
// (Name, Reasoning, Text, Keywords) that the integration tests will
// assert against. If these change, integration tests must be updated
// in lockstep.
func TestNewMessageStore_LoadsEmbeddedSamples(t *testing.T) {
	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore unexpected error: %v", err)
	}

	got := store.Messages()
	if len(got) != 3 {
		t.Fatalf("NewMessageStore loaded %d messages, want 3 (chat-only + farewell + greeting)", len(got))
	}

	// Sorted alphabetically: chat-only before farewell before greeting.
	if got[0].Name != "chat-only" {
		t.Fatalf("first message = %q, want chat-only", got[0].Name)
	}
	if got[1].Name != "farewell" {
		t.Fatalf("second message = %q, want farewell", got[1].Name)
	}
	if got[2].Name != "greeting" {
		t.Fatalf("third message = %q, want greeting", got[2].Name)
	}

	chatOnly := got[0]
	if chatOnly.Reasoning != "Responding with text only, no tools needed." {
		t.Errorf("chat-only reasoning = %q, want the no-tools reasoning", chatOnly.Reasoning)
	}
	if chatOnly.Text != "Sure, let's chat!" {
		t.Errorf("chat-only text = %q, want the chat text", chatOnly.Text)
	}
	if !slices.Contains(chatOnly.Keywords, "chat") {
		t.Errorf("chat-only keywords missing chat: %v", chatOnly.Keywords)
	}

	farewell := got[1]
	if farewell.Reasoning != "The user is saying goodbye." {
		t.Errorf("farewell reasoning = %q, want the goodbye reasoning", farewell.Reasoning)
	}
	if farewell.Text != "Goodbye! Have a great day!" {
		t.Errorf("farewell text = %q, want goodbye text", farewell.Text)
	}
	if !slices.Contains(farewell.Keywords, "bye") {
		t.Errorf("farewell keywords missing bye: %v", farewell.Keywords)
	}

	greeting := got[2]
	if greeting.Reasoning != "The user is greeting me, I should respond warmly." {
		t.Errorf("greeting reasoning = %q, want the warm greeting reasoning", greeting.Reasoning)
	}
	if greeting.Text != "Hello! How can I help you today?" {
		t.Errorf("greeting text = %q, want greeting text", greeting.Text)
	}
	if !slices.Contains(greeting.Keywords, "hello") {
		t.Errorf("greeting keywords missing hello: %v", greeting.Keywords)
	}
}

// TestNewMessageStore_LoadsEmbeddedTools verifies the embedded
// sample_tools.yaml is parsed into the store's Tools slice with the
// configured values, and sorted alphabetically by Name.
func TestNewMessageStore_LoadsEmbeddedTools(t *testing.T) {
	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore unexpected error: %v", err)
	}

	tools := store.Tools()
	if len(tools) != 5 {
		t.Fatalf("NewMessageStore loaded %d tools, want 5 (keyboard-success-text, mouse-click-button, mouse-followup-click, mouse-oob-click, mouse-success-text)", len(tools))
	}

	// Sorted alphabetically by Name.
	if tools[0].Name != "keyboard-success-text" {
		t.Errorf("tools[0] name = %q, want keyboard-success-text", tools[0].Name)
	}
	if tools[1].Name != "mouse-click-button" {
		t.Errorf("tools[1] name = %q, want mouse-click-button", tools[1].Name)
	}
	if tools[2].Name != "mouse-followup-click" {
		t.Errorf("tools[2] name = %q, want mouse-followup-click", tools[2].Name)
	}
	if tools[3].Name != "mouse-oob-click" {
		t.Errorf("tools[3] name = %q, want mouse-oob-click", tools[3].Name)
	}
	if tools[4].Name != "mouse-success-text" {
		t.Errorf("tools[4] name = %q, want mouse-success-text", tools[4].Name)
	}

	// mouse-click-button produces a LEFT_CLICK tool_call when the result
	// text contains "click here".
	clickButton := tools[1]
	if clickButton.ToolName != "mouse" {
		t.Errorf("mouse-click-button tool_name = %q, want mouse", clickButton.ToolName)
	}
	if !slices.Contains(clickButton.MatchResultContains, "click here") {
		t.Errorf("mouse-click-button match_result_contains missing 'click here': %v", clickButton.MatchResultContains)
	}
	if clickButton.RespondWith.ToolCall == nil {
		t.Fatalf("mouse-click-button respond_with.tool_call is nil")
	}
	if clickButton.RespondWith.ToolCall.Arguments["action"] != "LEFT_CLICK" {
		t.Errorf("mouse-click-button tool_call.arguments.action = %v, want LEFT_CLICK", clickButton.RespondWith.ToolCall.Arguments["action"])
	}

	// mouse-followup-click carries a tool_call response with arguments.
	mouseFollowup := tools[2]
	if mouseFollowup.ToolName != "mouse" {
		t.Errorf("mouse-followup-click tool_name = %q, want mouse", mouseFollowup.ToolName)
	}
	if !slices.Contains(mouseFollowup.MatchResultContains, "button") {
		t.Errorf("mouse-followup-click match_result_contains missing button: %v", mouseFollowup.MatchResultContains)
	}
	if mouseFollowup.RespondWith.ToolCall == nil {
		t.Fatalf("mouse-followup-click respond_with.tool_call is nil")
	}
	if mouseFollowup.RespondWith.ToolCall.Name != "mouse" {
		t.Errorf("mouse-followup-click tool_call.name = %q, want mouse", mouseFollowup.RespondWith.ToolCall.Name)
	}
	if mouseFollowup.RespondWith.ToolCall.Arguments["action"] != "LEFT_CLICK" {
		t.Errorf("mouse-followup-click tool_call.arguments.action = %v, want LEFT_CLICK", mouseFollowup.RespondWith.ToolCall.Arguments["action"])
	}

	// mouse-oob-click produces an out-of-bounds LEFT_CLICK tool_call.
	oobClick := tools[3]
	if oobClick.RespondWith.ToolCall == nil {
		t.Fatalf("mouse-oob-click respond_with.tool_call is nil")
	}
	if oobClick.RespondWith.ToolCall.Arguments["x_px"] != 99999 {
		t.Errorf("mouse-oob-click tool_call.arguments.x_px = %v, want 99999", oobClick.RespondWith.ToolCall.Arguments["x_px"])
	}

	// mouse-success-text carries a plain text response.
	mouseSuccess := tools[4]
	if mouseSuccess.RespondWith.Text != "I see the screen now." {
		t.Errorf("mouse-success-text respond_with.text = %q, want 'I see the screen now.'", mouseSuccess.RespondWith.Text)
	}
	if mouseSuccess.RespondWith.ToolCall != nil {
		t.Errorf("mouse-success-text respond_with.tool_call should be nil")
	}
}
