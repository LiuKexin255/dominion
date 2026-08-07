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
	if len(got) != 9 {
		t.Fatalf("NewMessageStore loaded %d messages, want 9 (chat-only + compress-planner-summary + compress-player-summary + farewell + greeting + mouse-trigger + planner-update-strategy + saolei-remain + saolei-start)", len(got))
	}

	// Sorted alphabetically: chat-only before compress-planner-summary before
	// compress-player-summary before farewell before greeting before
	// mouse-trigger before planner-update-strategy before saolei-remain
	// before saolei-start ("compress-planner-summary" < "compress-player-summary"
	// because 'n' < 'y' at the first differing rune; "planner-update-strategy"
	// < "saolei-remain" because 'p' < 's'; "saolei-remain" < "saolei-start"
	// because 'r' < 's').
	if got[0].Name != "chat-only" {
		t.Fatalf("first message = %q, want chat-only", got[0].Name)
	}
	if got[1].Name != "compress-planner-summary" {
		t.Fatalf("second message = %q, want compress-planner-summary", got[1].Name)
	}
	if got[2].Name != "compress-player-summary" {
		t.Fatalf("third message = %q, want compress-player-summary", got[2].Name)
	}
	if got[3].Name != "farewell" {
		t.Fatalf("fourth message = %q, want farewell", got[3].Name)
	}
	if got[4].Name != "greeting" {
		t.Fatalf("fifth message = %q, want greeting", got[4].Name)
	}
	if got[5].Name != "mouse-trigger" {
		t.Fatalf("sixth message = %q, want mouse-trigger", got[5].Name)
	}
	if got[6].Name != "planner-update-strategy" {
		t.Fatalf("seventh message = %q, want planner-update-strategy", got[6].Name)
	}
	if got[7].Name != "saolei-remain" {
		t.Fatalf("eighth message = %q, want saolei-remain", got[7].Name)
	}
	if got[8].Name != "saolei-start" {
		t.Fatalf("ninth message = %q, want saolei-start", got[8].Name)
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

	farewell := got[3]
	if farewell.Reasoning != "The user is saying goodbye." {
		t.Errorf("farewell reasoning = %q, want the goodbye reasoning", farewell.Reasoning)
	}
	if farewell.Text != "Goodbye! Have a great day!" {
		t.Errorf("farewell text = %q, want goodbye text", farewell.Text)
	}
	if !slices.Contains(farewell.Keywords, "bye") {
		t.Errorf("farewell keywords missing bye: %v", farewell.Keywords)
	}

	greeting := got[4]
	if greeting.Reasoning != "The user is greeting me, I should respond warmly." {
		t.Errorf("greeting reasoning = %q, want the warm greeting reasoning", greeting.Reasoning)
	}
	if greeting.Text != "Hello! How can I help you today?" {
		t.Errorf("greeting text = %q, want greeting text", greeting.Text)
	}
	if !slices.Contains(greeting.Keywords, "hello") {
		t.Errorf("greeting keywords missing hello: %v", greeting.Keywords)
	}

	// compress-planner-summary and compress-player-summary are the two
	// plain-text responses for the team graph's COMPRESS node summary calls
	// (specs/037-saolei-team-optimize US2 / FR-008/FR-012). The compress node
	// invokes the player/planner models directly (team/compress.ts
	// summarizeChannel) with the summary prompts + serialized channels; the
	// keywords below are substrings of those prompts' instruction lines, so
	// the calls match deterministically and the response TEXT becomes the
	// post-compression channel message and live summary frame
	// (helpers_test.go expectedPlayerCompressionSummary /
	// expectedPlannerCompressionSummary — keep in sync).
	compressPlanner := got[1]
	if compressPlanner.ToolCall != nil {
		t.Errorf("compress-planner-summary must carry a plain text response (a tool_call would abort compression — FR-012)")
	}
	if compressPlanner.Text != "已复盘 5 局，策略更新正常，每局均按新策略执行。" {
		t.Errorf("compress-planner-summary text = %q, want the pinned compression summary", compressPlanner.Text)
	}
	if !slices.Contains(compressPlanner.Keywords, "已复盘局数") {
		t.Errorf("compress-planner-summary keywords missing '已复盘局数': %v", compressPlanner.Keywords)
	}

	compressPlayer := got[2]
	if compressPlayer.ToolCall != nil {
		t.Errorf("compress-player-summary must carry a plain text response (a tool_call would abort compression — FR-012)")
	}
	if compressPlayer.Text != "已玩 5 局，其中 4 局失败。策略：优先翻开角落与边缘格子，命中数字 1 时先标记周围雷。" {
		t.Errorf("compress-player-summary text = %q, want the pinned compression summary", compressPlayer.Text)
	}
	if !slices.Contains(compressPlayer.Keywords, "已玩局数、胜负记录") {
		t.Errorf("compress-player-summary keywords missing '已玩局数、胜负记录': %v", compressPlayer.Keywords)
	}

	// mouse-trigger carries a tool_call (the dispatch fix): a user turn
	// matching its keyword makes fake-LLM return a mouse_move tool_call
	// so the agent_operation large tests drive the real dispatch chain.
	mouseTrigger := got[5]
	if mouseTrigger.ToolCall == nil {
		t.Fatalf("mouse-trigger tool_call is nil")
	}
	if mouseTrigger.ToolCall.Name != "mouse_move" {
		t.Errorf("mouse-trigger tool_call.name = %q, want mouse_move", mouseTrigger.ToolCall.Name)
	}
	if !slices.Contains(mouseTrigger.Keywords, "move the mouse") {
		t.Errorf("mouse-trigger keywords missing 'move the mouse': %v", mouseTrigger.Keywords)
	}

	// planner-update-strategy carries an update_strategy tool_call (spec
	// 031-team-template-mode FR-012): the team graph's planner agent — whose
	// review HumanMessage always starts with the fixed prefix "本局游戏过程"
	// (planner.ts buildReviewInput renders the gameLog — specs/036-team-
	// mode-bugfix/contracts/team-graph-fix-contract.md §2.2) — matches this
	// Message deterministically, so the saolei_team large tests drive the
	// planner→update_strategy→StrategyStore flow end-to-end.
	plannerStrategy := got[6]
	if plannerStrategy.ToolCall == nil {
		t.Fatalf("planner-update-strategy tool_call is nil")
	}
	if plannerStrategy.ToolCall.Name != "update_strategy" {
		t.Errorf("planner-update-strategy tool_call.name = %q, want update_strategy", plannerStrategy.ToolCall.Name)
	}
	if !slices.Contains(plannerStrategy.Keywords, "本局游戏过程") {
		t.Errorf("planner-update-strategy keywords missing the review prefix: %v", plannerStrategy.Keywords)
	}

	// saolei-remain carries a saolei_remain tool_call (spec 029 US2): a user
	// turn matching its keyword makes fake-LLM return a saolei_remain
	// tool_call so the agent_saolei large test drives the read-only remain
	// query end-to-end (specs/029-saolei-coord-remain/contracts/saolei-
	// remain-tool-contract.md §8).
	saoleiRemain := got[7]
	if saoleiRemain.ToolCall == nil {
		t.Fatalf("saolei-remain tool_call is nil")
	}
	if saoleiRemain.ToolCall.Name != "saolei_remain" {
		t.Errorf("saolei-remain tool_call.name = %q, want saolei_remain", saoleiRemain.ToolCall.Name)
	}
	if !slices.Contains(saoleiRemain.Keywords, "show remaining mines") {
		t.Errorf("saolei-remain keywords missing 'show remaining mines': %v", saoleiRemain.Keywords)
	}

	// saolei-start carries the first saolei_init tool_call (the entry
	// point of the agent_saolei large-test flow).
	saoleiStart := got[8]
	if saoleiStart.ToolCall == nil {
		t.Fatalf("saolei-start tool_call is nil")
	}
	if saoleiStart.ToolCall.Name != "saolei_init" {
		t.Errorf("saolei-start tool_call.name = %q, want saolei_init", saoleiStart.ToolCall.Name)
	}
	if !slices.Contains(saoleiStart.Keywords, "start saolei") {
		t.Errorf("saolei-start keywords missing 'start saolei': %v", saoleiStart.Keywords)
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
	if len(tools) != 12 {
		t.Fatalf("NewMessageStore loaded %d tools, want 12 (keyboard-success-text, mouse-click-button, mouse-click-success-text, mouse-move-followup-click, mouse-move-oob, mouse-move-success-text, saolei-click-3-4-followup-click, saolei-click-5-6-final-text, saolei-click-terminal-text, saolei-init-followup-click, saolei-remain-final-text, update-strategy-success-text)", len(tools))
	}

	// Sorted alphabetically by Name.
	wantNames := []string{
		"keyboard-success-text",
		"mouse-click-button",
		"mouse-click-success-text",
		"mouse-move-followup-click",
		"mouse-move-oob",
		"mouse-move-success-text",
		"saolei-click-3-4-followup-click",
		"saolei-click-5-6-final-text",
		"saolei-click-terminal-text",
		"saolei-init-followup-click",
		"saolei-remain-final-text",
		"update-strategy-success-text",
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

	// saolei-init-followup-click chains a saolei_init result into a
	// saolei_click{3,4} tool_call (specs/023-saolei-mcp-refine/quickstart.md
	// Scenario 5 — stateless init→click flow, no update).
	saoleiInitClick := tools[9]
	if saoleiInitClick.ToolName != "saolei_init" {
		t.Errorf("saolei-init-followup-click tool_name = %q, want saolei_init", saoleiInitClick.ToolName)
	}
	if saoleiInitClick.RespondWith.ToolCall == nil {
		t.Fatalf("saolei-init-followup-click respond_with.tool_call is nil")
	}
	if saoleiInitClick.RespondWith.ToolCall.Name != "saolei_click" {
		t.Errorf("saolei-init-followup-click tool_call.name = %q, want saolei_click", saoleiInitClick.RespondWith.ToolCall.Name)
	}

	// saolei-click-3-4-followup-click chains the first saolei_click{3,4}
	// result into a back-to-back saolei_click{5,6} tool_call (spec 023
	// FR-021 — tools callable back-to-back with no intervening step). The
	// match_result_contains=["(3,4)"] substring distinguishes this result
	// from the second click's "(5,6)" result.
	saoleiClick34 := tools[6]
	if saoleiClick34.Name != "saolei-click-3-4-followup-click" {
		t.Errorf("tools[6] name = %q, want saolei-click-3-4-followup-click", saoleiClick34.Name)
	}
	if saoleiClick34.ToolName != "saolei_click" {
		t.Errorf("saolei-click-3-4-followup-click tool_name = %q, want saolei_click", saoleiClick34.ToolName)
	}
	if !slices.Contains(saoleiClick34.MatchResultContains, "(3,4)") {
		t.Errorf("saolei-click-3-4-followup-click match_result_contains missing (3,4): %v", saoleiClick34.MatchResultContains)
	}
	if saoleiClick34.RespondWith.ToolCall == nil {
		t.Fatalf("saolei-click-3-4-followup-click respond_with.tool_call is nil")
	}
	if saoleiClick34.RespondWith.ToolCall.Name != "saolei_click" {
		t.Errorf("saolei-click-3-4-followup-click tool_call.name = %q, want saolei_click", saoleiClick34.RespondWith.ToolCall.Name)
	}

	// saolei-click-5-6-final-text terminates the saolei tool loop with
	// text after the second click's "(5,6)" result.
	saoleiClick56Final := tools[7]
	if saoleiClick56Final.Name != "saolei-click-5-6-final-text" {
		t.Errorf("tools[7] name = %q, want saolei-click-5-6-final-text", saoleiClick56Final.Name)
	}
	if saoleiClick56Final.ToolName != "saolei_click" {
		t.Errorf("saolei-click-5-6-final-text tool_name = %q, want saolei_click", saoleiClick56Final.ToolName)
	}
	if !slices.Contains(saoleiClick56Final.MatchResultContains, "(5,6)") {
		t.Errorf("saolei-click-5-6-final-text match_result_contains missing (5,6): %v", saoleiClick56Final.MatchResultContains)
	}
	if saoleiClick56Final.RespondWith.Text != "Minesweeper sequence complete." {
		t.Errorf("saolei-click-5-6-final-text respond_with.text = %q, want 'Minesweeper sequence complete.'", saoleiClick56Final.RespondWith.Text)
	}
	if saoleiClick56Final.RespondWith.ToolCall != nil {
		t.Errorf("saolei-click-5-6-final-text respond_with.tool_call should be nil")
	}

	// saolei-click-terminal-text terminates ANY saolei_click result that does
	// not match the coordinate-tagged configs (e.g. the pre-dispatch
	// rejections on terminal boards, whose bodies carry no "(x,y)") — it
	// keeps the saolei_team suite's post-rejection tool loop deterministic
	// instead of falling into the no-match random fallback.
	saoleiClickTerminal := tools[8]
	if saoleiClickTerminal.Name != "saolei-click-terminal-text" {
		t.Errorf("tools[8] name = %q, want saolei-click-terminal-text", saoleiClickTerminal.Name)
	}
	if saoleiClickTerminal.ToolName != "saolei_click" {
		t.Errorf("saolei-click-terminal-text tool_name = %q, want saolei_click", saoleiClickTerminal.ToolName)
	}
	if saoleiClickTerminal.RespondWith.Text != "Minesweeper sequence complete." {
		t.Errorf("saolei-click-terminal-text respond_with.text = %q, want 'Minesweeper sequence complete.'", saoleiClickTerminal.RespondWith.Text)
	}
	if saoleiClickTerminal.RespondWith.ToolCall != nil {
		t.Errorf("saolei-click-terminal-text respond_with.tool_call should be nil")
	}

	// saolei-remain-final-text terminates the saolei_remain tool loop with
	// a plain text response (spec 029 US2). saolei_remain dispatches
	// nothing, so the fake-LLM must return text after its result to end the
	// turn deterministically (otherwise the no-match random fallback could
	// emit an unrelated tool_call). tool_name=saolei_remain is unique to
	// this config.
	saoleiRemainFinal := tools[10]
	if saoleiRemainFinal.Name != "saolei-remain-final-text" {
		t.Errorf("tools[9] name = %q, want saolei-remain-final-text", saoleiRemainFinal.Name)
	}
	if saoleiRemainFinal.ToolName != "saolei_remain" {
		t.Errorf("saolei-remain-final-text tool_name = %q, want saolei_remain", saoleiRemainFinal.ToolName)
	}
	if saoleiRemainFinal.RespondWith.Text != "Remaining mines computed." {
		t.Errorf("saolei-remain-final-text respond_with.text = %q, want 'Remaining mines computed.'", saoleiRemainFinal.RespondWith.Text)
	}
	if saoleiRemainFinal.RespondWith.ToolCall != nil {
		t.Errorf("saolei-remain-final-text respond_with.tool_call should be nil")
	}

	// update-strategy-success-text terminates the planner agent's
	// update_strategy tool loop with a plain text response (spec
	// 031-team-template-mode FR-012/D6). tool_name=update_strategy is unique
	// to this config, so it is the only MatchToolResult candidate for an
	// update_strategy result — deterministic, no random fallback.
	updateStrategyFinal := tools[11]
	if updateStrategyFinal.Name != "update-strategy-success-text" {
		t.Errorf("tools[10] name = %q, want update-strategy-success-text", updateStrategyFinal.Name)
	}
	if updateStrategyFinal.ToolName != "update_strategy" {
		t.Errorf("update-strategy-success-text tool_name = %q, want update_strategy", updateStrategyFinal.ToolName)
	}
	if updateStrategyFinal.RespondWith.Text != "策略已更新，下一局将按新策略执行。" {
		t.Errorf("update-strategy-success-text respond_with.text = %q, want '策略已更新，下一局将按新策略执行。'", updateStrategyFinal.RespondWith.Text)
	}
	if updateStrategyFinal.RespondWith.ToolCall != nil {
		t.Errorf("update-strategy-success-text respond_with.tool_call should be nil")
	}
}
