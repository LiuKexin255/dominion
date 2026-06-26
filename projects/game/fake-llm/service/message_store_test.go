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
//
// Feature 015 split the single "mouse" tool into "mouse_move"
// (coordinates) and "mouse_click" (click_type only), so the tool_name
// and tool_call argument shapes below reflect the split.
func TestNewMessageStore_LoadsEmbeddedTools(t *testing.T) {
	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore unexpected error: %v", err)
	}

	tools := store.Tools()
	if len(tools) != 6 {
		t.Fatalf("NewMessageStore loaded %d tools, want 6 (keyboard-success-text, mouse-click-button, mouse-click-success-text, mouse-move-followup-click, mouse-move-oob, mouse-move-success-text)", len(tools))
	}

	// Sorted alphabetically by Name.
	wantNames := []string{
		"keyboard-success-text",
		"mouse-click-button",
		"mouse-click-success-text",
		"mouse-move-followup-click",
		"mouse-move-oob",
		"mouse-move-success-text",
	}
	for i, want := range wantNames {
		if tools[i].Name != want {
			t.Errorf("tools[%d] name = %q, want %q", i, tools[i].Name, want)
		}
	}

	// mouse-click-button produces a LEFT_CLICK mouse_click tool_call when
	// the result text contains "click here". After the US2 split a click
	// carries only click_type (no coordinates).
	clickButton := tools[1]
	if clickButton.ToolName != "mouse_click" {
		t.Errorf("mouse-click-button tool_name = %q, want mouse_click", clickButton.ToolName)
	}
	if !slices.Contains(clickButton.MatchResultContains, "click here") {
		t.Errorf("mouse-click-button match_result_contains missing 'click here': %v", clickButton.MatchResultContains)
	}
	if clickButton.RespondWith.ToolCall == nil {
		t.Fatalf("mouse-click-button respond_with.tool_call is nil")
	}
	if clickButton.RespondWith.ToolCall.Name != "mouse_click" {
		t.Errorf("mouse-click-button tool_call.name = %q, want mouse_click", clickButton.RespondWith.ToolCall.Name)
	}
	if clickButton.RespondWith.ToolCall.Arguments["click_type"] != "LEFT_CLICK" {
		t.Errorf("mouse-click-button tool_call.arguments.click_type = %v, want LEFT_CLICK", clickButton.RespondWith.ToolCall.Arguments["click_type"])
	}

	// mouse-click-success-text carries a plain text response.
	clickSuccess := tools[2]
	if clickSuccess.ToolName != "mouse_click" {
		t.Errorf("mouse-click-success-text tool_name = %q, want mouse_click", clickSuccess.ToolName)
	}
	if clickSuccess.RespondWith.Text != "Clicked successfully." {
		t.Errorf("mouse-click-success-text respond_with.text = %q, want 'Clicked successfully.'", clickSuccess.RespondWith.Text)
	}
	if clickSuccess.RespondWith.ToolCall != nil {
		t.Errorf("mouse-click-success-text respond_with.tool_call should be nil")
	}

	// mouse-move-followup-click chains a move result into a click tool_call.
	moveFollowup := tools[3]
	if moveFollowup.ToolName != "mouse_move" {
		t.Errorf("mouse-move-followup-click tool_name = %q, want mouse_move", moveFollowup.ToolName)
	}
	if !slices.Contains(moveFollowup.MatchResultContains, "button") {
		t.Errorf("mouse-move-followup-click match_result_contains missing button: %v", moveFollowup.MatchResultContains)
	}
	if moveFollowup.RespondWith.ToolCall == nil {
		t.Fatalf("mouse-move-followup-click respond_with.tool_call is nil")
	}
	if moveFollowup.RespondWith.ToolCall.Name != "mouse_click" {
		t.Errorf("mouse-move-followup-click tool_call.name = %q, want mouse_click", moveFollowup.RespondWith.ToolCall.Name)
	}

	// mouse-move-oob produces an out-of-bounds mouse_move tool_call with
	// coordinates (clicks carry no coordinates after the US2 split).
	moveOob := tools[4]
	if moveOob.ToolName != "mouse_move" {
		t.Errorf("mouse-move-oob tool_name = %q, want mouse_move", moveOob.ToolName)
	}
	if moveOob.RespondWith.ToolCall == nil {
		t.Fatalf("mouse-move-oob respond_with.tool_call is nil")
	}
	if moveOob.RespondWith.ToolCall.Arguments["x_px"] != 99999 {
		t.Errorf("mouse-move-oob tool_call.arguments.x_px = %v, want 99999", moveOob.RespondWith.ToolCall.Arguments["x_px"])
	}
	if moveOob.RespondWith.ToolCall.Arguments["y_px"] != 99999 {
		t.Errorf("mouse-move-oob tool_call.arguments.y_px = %v, want 99999", moveOob.RespondWith.ToolCall.Arguments["y_px"])
	}

	// mouse-move-success-text carries a plain text response.
	moveSuccess := tools[5]
	if moveSuccess.ToolName != "mouse_move" {
		t.Errorf("mouse-move-success-text tool_name = %q, want mouse_move", moveSuccess.ToolName)
	}
	if moveSuccess.RespondWith.Text != "I see the screen now." {
		t.Errorf("mouse-move-success-text respond_with.text = %q, want 'I see the screen now.'", moveSuccess.RespondWith.Text)
	}
	if moveSuccess.RespondWith.ToolCall != nil {
		t.Errorf("mouse-move-success-text respond_with.tool_call should be nil")
	}
}
