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
// in lockstep (projects/game/testplan/README.md §5).
func TestNewMessageStore_LoadsEmbeddedSamples(t *testing.T) {
	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore unexpected error: %v", err)
	}

	got := store.Messages()
	if len(got) != 13 {
		t.Fatalf("NewMessageStore loaded %d messages, want 13 (chat-only + compact-instruction + compress-planner-summary + compress-player-summary + farewell + greeting + init-instruction + mouse-trigger + planner-memory-add + saolei-remain + saolei-single-op + saolei-start + saolei-structural-stop)", len(got))
	}

	// Sorted alphabetically: chat-only before compact-instruction before
	// compress-planner-summary before compress-player-summary before
	// farewell before greeting before init-instruction before mouse-trigger
	// before planner-memory-add before saolei-remain before saolei-single-op
	// before saolei-start before saolei-structural-stop
	// ("compact-instruction" < "compress-planner-summary" because 'a' < 'r'
	// at the first differing rune; "planner-memory-add" < "saolei-remain"
	// because 'p' < 's'; "saolei-single-op" < "saolei-start" because 'i' <
	// 't'; "saolei-start" < "saolei-structural-stop" because 'a' < 'r').
	wantNames := []string{
		"chat-only",
		"compact-instruction",
		"compress-planner-summary",
		"compress-player-summary",
		"farewell",
		"greeting",
		"init-instruction",
		"mouse-trigger",
		"planner-memory-add",
		"saolei-remain",
		"saolei-single-op",
		"saolei-start",
		"saolei-structural-stop",
	}
	for i, want := range wantNames {
		if got[i].Name != want {
			t.Fatalf("message[%d] = %q, want %q", i, got[i].Name, want)
		}
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

	farewell := got[4]
	if farewell.Reasoning != "The user is saying goodbye." {
		t.Errorf("farewell reasoning = %q, want the goodbye reasoning", farewell.Reasoning)
	}
	if farewell.Text != "Goodbye! Have a great day!" {
		t.Errorf("farewell text = %q, want goodbye text", farewell.Text)
	}
	if !slices.Contains(farewell.Keywords, "bye") {
		t.Errorf("farewell keywords missing bye: %v", farewell.Keywords)
	}

	greeting := got[5]
	if greeting.Reasoning != "The user is greeting me, I should respond warmly." {
		t.Errorf("greeting reasoning = %q, want the warm greeting reasoning", greeting.Reasoning)
	}
	if greeting.Text != "Hello! How can I help you today?" {
		t.Errorf("greeting text = %q, want greeting text", greeting.Text)
	}
	if !slices.Contains(greeting.Keywords, "hello") {
		t.Errorf("greeting keywords missing hello: %v", greeting.Keywords)
	}

	// compact-instruction / init-instruction are the two instruction-node
	// scenario fixtures (spec 039-planner-memory-calibration US3 — FR-015/
	// FR-016/FR-019): the initInstruction / postCompactInstruction nodes
	// (team/instruction-node.ts buildInstructionRequest) end their model
	// input with a request HumanMessage starting "团队初始化：" /
	// "上下文刚被压缩：", so keyword-matching those prefixes returns an
	// instruct_player tool_call deterministically. The contents are pinned
	// in helpers_test.go (expectedInitInstructionText /
	// expectedCompactInstructionText — keep in sync).
	initInstruction := got[6]
	if initInstruction.ToolCall == nil {
		t.Fatalf("init-instruction tool_call is nil")
	}
	if initInstruction.ToolCall.Name != "instruct_player" {
		t.Errorf("init-instruction tool_call.name = %q, want instruct_player", initInstruction.ToolCall.Name)
	}
	if !slices.Contains(initInstruction.Keywords, "团队初始化") {
		t.Errorf("init-instruction keywords missing the init request prefix: %v", initInstruction.Keywords)
	}
	if initInstruction.ToolCall.Arguments["content"] != "初始指令：先点中心区域，再按数字展开。" {
		t.Errorf("init-instruction content = %v, want the pinned initial instruction", initInstruction.ToolCall.Arguments["content"])
	}

	compactInstruction := got[1]
	if compactInstruction.ToolCall == nil {
		t.Fatalf("compact-instruction tool_call is nil")
	}
	if compactInstruction.ToolCall.Name != "instruct_player" {
		t.Errorf("compact-instruction tool_call.name = %q, want instruct_player", compactInstruction.ToolCall.Name)
	}
	if !slices.Contains(compactInstruction.Keywords, "上下文刚被压缩") {
		t.Errorf("compact-instruction keywords missing the compact request prefix: %v", compactInstruction.Keywords)
	}
	if compactInstruction.ToolCall.Arguments["content"] != "压缩后指令：保持节奏，先 deduce 再 flag。请继续游戏。" {
		t.Errorf("compact-instruction content = %v, want the pinned compact instruction", compactInstruction.ToolCall.Arguments["content"])
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
	compressPlanner := got[2]
	if compressPlanner.ToolCall != nil {
		t.Errorf("compress-planner-summary must carry a plain text response (a tool_call would abort compression — FR-012)")
	}
	if compressPlanner.Text != "已复盘 5 局，长期记忆更新正常。" {
		t.Errorf("compress-planner-summary text = %q, want the pinned compression summary", compressPlanner.Text)
	}
	if !slices.Contains(compressPlanner.Keywords, "已复盘局数") {
		t.Errorf("compress-planner-summary keywords missing '已复盘局数': %v", compressPlanner.Keywords)
	}

	compressPlayer := got[3]
	if compressPlayer.ToolCall != nil {
		t.Errorf("compress-player-summary must carry a plain text response (a tool_call would abort compression — FR-012)")
	}
	if compressPlayer.Text != "已玩 5 局，其中 4 局失败。下一局按复盘指令调整打法。" {
		t.Errorf("compress-player-summary text = %q, want the pinned compression summary", compressPlayer.Text)
	}
	if !slices.Contains(compressPlayer.Keywords, "已玩局数、胜负记录") {
		t.Errorf("compress-player-summary keywords missing '已玩局数、胜负记录': %v", compressPlayer.Keywords)
	}

	// mouse-trigger carries a tool_call (the dispatch fix): a user turn
	// matching its keyword makes fake-LLM return a mouse_move tool_call
	// so the agent_operation large tests drive the real dispatch chain.
	mouseTrigger := got[7]
	if mouseTrigger.ToolCall == nil {
		t.Fatalf("mouse-trigger tool_call is nil")
	}
	if mouseTrigger.ToolCall.Name != "mouse_move" {
		t.Errorf("mouse-trigger tool_call.name = %q, want mouse_move", mouseTrigger.ToolCall.Name)
	}
	if !slices.Contains(mouseTrigger.Keywords, "move the mouse") {
		t.Errorf("mouse-trigger keywords missing 'move the mouse': %v", mouseTrigger.Keywords)
	}

	// planner-memory-add carries a hermes-style `memory` BATCH tool_call
	// (spec 039-planner-memory-calibration FR-008 — action/content/old_text/
	// operations, NO memory_id/target): the team graph's planner agent — whose
	// review HumanMessage always starts with the fixed prefix "本局游戏过程"
	// (planner.ts buildReviewInput renders the gameLog — specs/036-team-mode-
	// bugfix/contracts/team-graph-fix-contract.md §2.2) — matches this Message
	// deterministically, so the saolei_team / memory large tests drive the
	// planner→memory→MemoryService flow end-to-end. The former
	// update_strategy fixture (spec 031 FR-012) is gone (FR-013 — Phase 6).
	plannerMemory := got[8]
	if plannerMemory.ToolCall == nil {
		t.Fatalf("planner-memory-add tool_call is nil")
	}
	if plannerMemory.ToolCall.Name != "memory" {
		t.Errorf("planner-memory-add tool_call.name = %q, want memory", plannerMemory.ToolCall.Name)
	}
	if !slices.Contains(plannerMemory.Keywords, "本局游戏过程") {
		t.Errorf("planner-memory-add keywords missing the review prefix: %v", plannerMemory.Keywords)
	}
	if plannerMemory.ToolCall.Arguments["operations"] == nil {
		t.Errorf("planner-memory-add tool_call must carry the operations batch (FR-008)")
	}
	if _, hasMemoryID := plannerMemory.ToolCall.Arguments["memory_id"]; hasMemoryID {
		t.Errorf("planner-memory-add tool_call must NOT carry memory_id (FR-008)")
	}
	if _, hasTarget := plannerMemory.ToolCall.Arguments["target"]; hasTarget {
		t.Errorf("planner-memory-add tool_call must NOT carry target (FR-008)")
	}

	// saolei-remain carries a saolei_remain tool_call (spec 029 US2): a user
	// turn matching its keyword makes fake-LLM return a saolei_remain
	// tool_call so the agent_saolei large test drives the read-only remain
	// query end-to-end (specs/029-saolei-coord-remain/contracts/saolei-
	// remain-tool-contract.md §8).
	saoleiRemain := got[9]
	if saoleiRemain.ToolCall == nil {
		t.Fatalf("saolei-remain tool_call is nil")
	}
	if saoleiRemain.ToolCall.Name != "saolei_remain" {
		t.Errorf("saolei-remain tool_call.name = %q, want saolei_remain", saoleiRemain.ToolCall.Name)
	}
	if !slices.Contains(saoleiRemain.Keywords, "show remaining mines") {
		t.Errorf("saolei-remain keywords missing 'show remaining mines': %v", saoleiRemain.Keywords)
	}

	// saolei-single-op carries a SINGLE-FORM saolei_operate tool_call (spec
	// 039 US1 — FR-001 dual form: ordinary type/x/y == a length-1 batch):
	// used by the agent_saolei dual-form-equivalence test's second turn.
	saoleiSingle := got[10]
	if saoleiSingle.ToolCall == nil {
		t.Fatalf("saolei-single-op tool_call is nil")
	}
	if saoleiSingle.ToolCall.Name != "saolei_operate" {
		t.Errorf("saolei-single-op tool_call.name = %q, want saolei_operate", saoleiSingle.ToolCall.Name)
	}
	if saoleiSingle.ToolCall.Arguments["type"] != "click" || saoleiSingle.ToolCall.Arguments["x"] != 3 || saoleiSingle.ToolCall.Arguments["y"] != 4 {
		t.Errorf("saolei-single-op tool_call arguments = %v, want type=click x=3 y=4", saoleiSingle.ToolCall.Arguments)
	}

	// saolei-start carries the first saolei_init tool_call (the entry
	// point of the agent_saolei large-test flow). The "继续" keyword also
	// matches the calibration instruction contents ("请继续游戏。" — spec
	// 039 FR-017: the appended review instruction becomes the player's last
	// user message, so the continuation keyword keeps the multi-game flow
	// deterministic).
	saoleiStart := got[11]
	if saoleiStart.ToolCall == nil {
		t.Fatalf("saolei-start tool_call is nil")
	}
	if saoleiStart.ToolCall.Name != "saolei_init" {
		t.Errorf("saolei-start tool_call.name = %q, want saolei_init", saoleiStart.ToolCall.Name)
	}
	if !slices.Contains(saoleiStart.Keywords, "start saolei") {
		t.Errorf("saolei-start keywords missing 'start saolei': %v", saoleiStart.Keywords)
	}
	if !slices.Contains(saoleiStart.Keywords, "继续") {
		t.Errorf("saolei-start keywords missing the instruction-continuation keyword '继续': %v", saoleiStart.Keywords)
	}

	// saolei-structural-stop carries a saolei_operate batch whose second op
	// is out-of-bounds (spec 039 US1 — FR-002 structural stop): used by the
	// agent_saolei structural-stop test's second turn.
	saoleiStructural := got[12]
	if saoleiStructural.ToolCall == nil {
		t.Fatalf("saolei-structural-stop tool_call is nil")
	}
	if saoleiStructural.ToolCall.Name != "saolei_operate" {
		t.Errorf("saolei-structural-stop tool_call.name = %q, want saolei_operate", saoleiStructural.ToolCall.Name)
	}
	if !slices.Contains(saoleiStructural.Keywords, "structural stop") {
		t.Errorf("saolei-structural-stop keywords missing 'structural stop': %v", saoleiStructural.Keywords)
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
	if len(tools) != 13 {
		t.Fatalf("NewMessageStore loaded %d tools, want 13 (instruct-player-final-text, keyboard-success-text, mouse-click-button, mouse-click-success-text, mouse-move-followup-click, mouse-move-oob, mouse-move-success-text, planner-memory-0-hit, planner-memory-applied, planner-memory-multi-hit, saolei-init-followup-operate, saolei-operate-final-text, saolei-remain-final-text)", len(tools))
	}

	// Sorted alphabetically by Name.
	wantNames := []string{
		"instruct-player-final-text",
		"keyboard-success-text",
		"mouse-click-button",
		"mouse-click-success-text",
		"mouse-move-followup-click",
		"mouse-move-oob",
		"mouse-move-success-text",
		"planner-memory-0-hit",
		"planner-memory-applied",
		"planner-memory-multi-hit",
		"saolei-init-followup-operate",
		"saolei-operate-final-text",
		"saolei-remain-final-text",
	}
	for i, want := range wantNames {
		if tools[i].Name != want {
			t.Errorf("tools[%d] name = %q, want %q", i, tools[i].Name, want)
		}
	}

	// mouse-click-button produces a LEFT_CLICK mouse_click tool_call when
	// the result text contains "click here". After the US2 split a click
	// carries only click_type (no coordinates).
	clickButton := tools[2]
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
	clickSuccess := tools[3]
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
	moveFollowup := tools[4]
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
	moveOob := tools[5]
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
	moveSuccess := tools[6]
	if moveSuccess.ToolName != "mouse_move" {
		t.Errorf("mouse-move-success-text tool_name = %q, want mouse_move", moveSuccess.ToolName)
	}
	if moveSuccess.RespondWith.Text != "I see the screen now." {
		t.Errorf("mouse-move-success-text respond_with.text = %q, want 'I see the screen now.'", moveSuccess.RespondWith.Text)
	}
	if moveSuccess.RespondWith.ToolCall != nil {
		t.Errorf("mouse-move-success-text respond_with.tool_call should be nil")
	}

	// planner-memory-applied / planner-memory-0-hit / planner-memory-multi-hit
	// are the planner's memory tool-loop chain (spec 039-planner-memory-
	// calibration FR-008 — the hermes-style agent conversion, memory-mcp.ts
	// applyMemoryCall/applyBatch): the batch-add result "memory: applied N
	// operation(s)" chains the 0-hit replace (old_text "无此内容"), whose "no
	// entry matched" error text chains the multi-hit replace (old_text "player
	// 常犯"), whose "multiple entries matched" error text chains
	// instruct_player (the review instruction, FR-014). The three memory
	// configs are mutually exclusive on the result bodies (verified in
	// sample_planner_tools.yaml).
	plannerMemoryApplied := tools[8]
	if plannerMemoryApplied.ToolName != "memory" {
		t.Errorf("planner-memory-applied tool_name = %q, want memory", plannerMemoryApplied.ToolName)
	}
	if !slices.Contains(plannerMemoryApplied.MatchResultContains, "operation(s)") {
		t.Errorf("planner-memory-applied match_result_contains missing 'operation(s)': %v", plannerMemoryApplied.MatchResultContains)
	}
	if plannerMemoryApplied.RespondWith.ToolCall == nil {
		t.Fatalf("planner-memory-applied respond_with.tool_call is nil")
	}
	if plannerMemoryApplied.RespondWith.ToolCall.Name != "memory" {
		t.Errorf("planner-memory-applied tool_call.name = %q, want memory", plannerMemoryApplied.RespondWith.ToolCall.Name)
	}
	if plannerMemoryApplied.RespondWith.ToolCall.Arguments["action"] != "replace" {
		t.Errorf("planner-memory-applied tool_call.arguments.action = %v, want replace", plannerMemoryApplied.RespondWith.ToolCall.Arguments["action"])
	}

	plannerMemory0Hit := tools[7]
	if plannerMemory0Hit.ToolName != "memory" {
		t.Errorf("planner-memory-0-hit tool_name = %q, want memory", plannerMemory0Hit.ToolName)
	}
	if !slices.Contains(plannerMemory0Hit.MatchResultContains, "no entry matched") {
		t.Errorf("planner-memory-0-hit match_result_contains missing 'no entry matched': %v", plannerMemory0Hit.MatchResultContains)
	}
	if plannerMemory0Hit.RespondWith.ToolCall == nil {
		t.Fatalf("planner-memory-0-hit respond_with.tool_call is nil")
	}
	if plannerMemory0Hit.RespondWith.ToolCall.Arguments["old_text"] != "player 常犯" {
		t.Errorf("planner-memory-0-hit tool_call.arguments.old_text = %v, want 'player 常犯'", plannerMemory0Hit.RespondWith.ToolCall.Arguments["old_text"])
	}

	plannerMemoryMultiHit := tools[9]
	if plannerMemoryMultiHit.ToolName != "memory" {
		t.Errorf("planner-memory-multi-hit tool_name = %q, want memory", plannerMemoryMultiHit.ToolName)
	}
	if !slices.Contains(plannerMemoryMultiHit.MatchResultContains, "multiple entries matched") {
		t.Errorf("planner-memory-multi-hit match_result_contains missing 'multiple entries matched': %v", plannerMemoryMultiHit.MatchResultContains)
	}
	if plannerMemoryMultiHit.RespondWith.ToolCall == nil {
		t.Fatalf("planner-memory-multi-hit respond_with.tool_call is nil")
	}
	if plannerMemoryMultiHit.RespondWith.ToolCall.Name != "instruct_player" {
		t.Errorf("planner-memory-multi-hit tool_call.name = %q, want instruct_player", plannerMemoryMultiHit.RespondWith.ToolCall.Name)
	}

	// instruct-player-final-text terminates ANY instruct_player tool loop
	// with a plain text response (review / init / compact scenarios share
	// it — spec 039 FR-014/FR-015/FR-016). tool_name=instruct_player is
	// unique to this config, so it is the only MatchToolResult candidate for
	// an instruct_player result — deterministic, no random fallback.
	instructPlayerFinal := tools[0]
	if instructPlayerFinal.ToolName != "instruct_player" {
		t.Errorf("instruct-player-final-text tool_name = %q, want instruct_player", instructPlayerFinal.ToolName)
	}
	if instructPlayerFinal.RespondWith.Text != "指令已发送。" {
		t.Errorf("instruct-player-final-text respond_with.text = %q, want '指令已发送。'", instructPlayerFinal.RespondWith.Text)
	}
	if instructPlayerFinal.RespondWith.ToolCall != nil {
		t.Errorf("instruct-player-final-text respond_with.tool_call should be nil")
	}

	// saolei-init-followup-operate chains a saolei_init result into a
	// saolei_operate BATCH tool_call (operations: [click{3,4}, click{5,6}])
	// — spec 039-planner-memory-calibration US1 (FR-001/FR-002): the merged
	// dual-form tool executes both ops IN ORDER in one call and returns once.
	saoleiInitOperate := tools[10]
	if saoleiInitOperate.ToolName != "saolei_init" {
		t.Errorf("saolei-init-followup-operate tool_name = %q, want saolei_init", saoleiInitOperate.ToolName)
	}
	if saoleiInitOperate.RespondWith.ToolCall == nil {
		t.Fatalf("saolei-init-followup-operate respond_with.tool_call is nil")
	}
	if saoleiInitOperate.RespondWith.ToolCall.Name != "saolei_operate" {
		t.Errorf("saolei-init-followup-operate tool_call.name = %q, want saolei_operate", saoleiInitOperate.RespondWith.ToolCall.Name)
	}

	// saolei-operate-final-text terminates ANY saolei_operate result with a
	// plain text response (the executed/skipped/stopped outcome lines and
	// the rejection bodies — contract saolei-operate-contract.md §2). It
	// keeps the post-operate tool loop deterministic under the team model
	// instead of falling into the no-match random fallback (whose pool
	// includes mouse tool_calls the team's player agent does not hold;
	// FR-028).
	saoleiOperateFinal := tools[11]
	if saoleiOperateFinal.Name != "saolei-operate-final-text" {
		t.Errorf("tools[11] name = %q, want saolei-operate-final-text", saoleiOperateFinal.Name)
	}
	if saoleiOperateFinal.ToolName != "saolei_operate" {
		t.Errorf("saolei-operate-final-text tool_name = %q, want saolei_operate", saoleiOperateFinal.ToolName)
	}
	if saoleiOperateFinal.RespondWith.Text != "Minesweeper sequence complete." {
		t.Errorf("saolei-operate-final-text respond_with.text = %q, want 'Minesweeper sequence complete.'", saoleiOperateFinal.RespondWith.Text)
	}
	if saoleiOperateFinal.RespondWith.ToolCall != nil {
		t.Errorf("saolei-operate-final-text respond_with.tool_call should be nil")
	}

	// saolei-remain-final-text terminates the saolei_remain tool loop with
	// a plain text response (spec 029 US2). saolei_remain dispatches
	// nothing, so the fake-LLM must return text after its result to end the
	// turn deterministically (otherwise the no-match random fallback could
	// emit an unrelated tool_call). tool_name=saolei_remain is unique to
	// this config.
	saoleiRemainFinal := tools[12]
	if saoleiRemainFinal.Name != "saolei-remain-final-text" {
		t.Errorf("tools[12] name = %q, want saolei-remain-final-text", saoleiRemainFinal.Name)
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
}
