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

// TestLoadFromFS_MultiMessageFile covers the multi-message file shape
// (specs/046-fake-llm-think-chunking/quickstart.md Scenario 6,
// FR-012/FR-013/FR-014): a `messages:` file merges into the flat slice
// indistinguishably from single-message files (sorted by Name), a
// duplicate name across a multi-message file and a single-message file
// is rejected, and a file declaring both `tools:` and `messages:` is
// rejected (validation rule V6, specs/046-fake-llm-think-chunking/
// research.md D4).
func TestLoadFromFS_MultiMessageFile(t *testing.T) {
	tests := []struct {
		name    string
		files   fstest.MapFS
		want    []string // sorted Names when loading succeeds
		wantErr string
	}{
		{
			name: "multi-message file merges with single-message file sorted by name",
			files: fstest.MapFS{
				"testdata/multi.yaml": &fstest.MapFile{
					// given: a multi-message file whose entries are NOT
					// alphabetically ordered, so the merge sort is exercised.
					Data: []byte(strings.Join([]string{
						"messages:",
						"  - name: beta",
						"    keywords: [k1]",
						"    reasoning: beta-reasoning",
						"    text: beta-text",
						"  - name: alpha",
						"    keywords: [k2]",
						"    reasoning: alpha-reasoning",
						"    text: alpha-text",
						"",
					}, "\n")),
				},
				"testdata/single.json": &fstest.MapFile{
					Data: []byte(`{"name":"gamma","keywords":["k3"],"reasoning":"gamma-reasoning","text":"gamma-text"}`),
				},
			},
			want: []string{"alpha", "beta", "gamma"},
		},
		{
			name: "duplicate name across multi-message and single-message files rejected",
			files: fstest.MapFS{
				"testdata/multi.yaml": &fstest.MapFile{
					Data: []byte(strings.Join([]string{
						"messages:",
						"  - name: dup",
						"    keywords: [k1]",
						"    reasoning: r",
						"    text: t",
						"",
					}, "\n")),
				},
				"testdata/single.yaml": &fstest.MapFile{
					Data: []byte("name: dup\nkeywords: [k2]\nreasoning: r\ntext: t\n"),
				},
			},
			wantErr: "duplicate",
		},
		{
			name: "file with both tools: and messages: rejected (V6)",
			files: fstest.MapFS{
				"testdata/both.yaml": &fstest.MapFile{
					Data: []byte(strings.Join([]string{
						"tools:",
						"  - name: t1",
						"    tool_name: mouse_move",
						"    match_result_contains: []",
						"    respond_with:",
						"      text: done",
						"messages:",
						"  - name: m1",
						"    keywords: [k1]",
						"    text: t",
						"",
					}, "\n")),
				},
			},
			wantErr: "both tools: and messages:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, tools, err := LoadFromFS(tt.files, "testdata")

			// then: a malformed shape aborts loading with a descriptive
			// error; otherwise every file's entries merge into one flat
			// slice sorted alphabetically by Name, with no tools loaded.
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("LoadFromFS expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("LoadFromFS error = %q, want substring %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadFromFS unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("LoadFromFS got %d messages, want %d", len(got), len(tt.want))
			}
			for i, wantName := range tt.want {
				if got[i].Name != wantName {
					t.Fatalf("LoadFromFS order = [%s...], want %v", got[i].Name, tt.want)
				}
			}
			if got[0].Text != "alpha-text" {
				t.Fatalf("LoadFromFS multi-message entry values wrong: %+v", got[0])
			}
			if tools != nil {
				t.Fatalf("LoadFromFS tools = %v, want nil for message-only files", tools)
			}
		})
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
		{
			name: "valid chunked message with delays",
			msgs: []*Message{{
				Name:            "a",
				Keywords:        []string{"x"},
				ReasoningChunks: []string{"c0", "c1", "c2"},
				ChunkDelays:     []string{"100ms", "200ms"},
			}},
			wantErr: "",
		},
		{
			name: "chunk_delays shorter than chunks-1 defaults missing gaps",
			msgs: []*Message{{
				Name:            "a",
				Keywords:        []string{"x"},
				ReasoningChunks: []string{"c0", "c1", "c2"},
				ChunkDelays:     []string{"100ms"},
			}},
			wantErr: "",
		},
		{
			name: "empty reasoning_chunks entry rejected (V1)",
			msgs: []*Message{{
				Name:            "a",
				Keywords:        []string{"x"},
				ReasoningChunks: []string{"c0", ""},
			}},
			wantErr: "empty reasoning_chunks entry",
		},
		{
			name: "chunk_delays longer than chunks-1 rejected (V2)",
			msgs: []*Message{{
				Name:            "a",
				Keywords:        []string{"x"},
				ReasoningChunks: []string{"c0", "c1"},
				ChunkDelays:     []string{"100ms", "200ms", "300ms"},
			}},
			wantErr: "chunk_delays",
		},
		{
			name: "chunk_delays without reasoning_chunks rejected (V2)",
			msgs: []*Message{{
				Name:        "a",
				Keywords:    []string{"x"},
				ChunkDelays: []string{"100ms"},
			}},
			wantErr: "chunk_delays",
		},
		{
			name: "unparseable chunk_delays entry rejected (V2)",
			msgs: []*Message{{
				Name:            "a",
				Keywords:        []string{"x"},
				ReasoningChunks: []string{"c0", "c1"},
				ChunkDelays:     []string{"oops"},
			}},
			wantErr: "unparseable chunk_delays",
		},
		{
			name: "both reasoning and reasoning_chunks rejected (V4)",
			msgs: []*Message{{
				Name:            "a",
				Keywords:        []string{"x"},
				Reasoning:       "legacy",
				ReasoningChunks: []string{"c0"},
			}},
			wantErr: "both reasoning and reasoning_chunks",
		},
		{
			name: "tool_call with reasoning_chunks rejected (V5)",
			msgs: []*Message{{
				Name:            "a",
				Keywords:        []string{"x"},
				ReasoningChunks: []string{"c0"},
				ToolCall:        &ToolCall{Name: "t"},
			}},
			wantErr: "tool_call",
		},
		{
			name: "tool_call with chunk_delays rejected (V5)",
			msgs: []*Message{{
				Name:            "a",
				Keywords:        []string{"x"},
				ReasoningChunks: []string{"c0", "c1"},
				ChunkDelays:     []string{"100ms"},
				ToolCall:        &ToolCall{Name: "t"},
			}},
			wantErr: "tool_call",
		},
		{
			name: "in-range stall_after accepted (V3)",
			msgs: []*Message{{
				Name:            "a",
				Keywords:        []string{"x"},
				ReasoningChunks: []string{"c0", "c1", "c2"},
				StallAfter:      toPtr(2),
			}},
			wantErr: "",
		},
		{
			name: "legacy single-reasoning stall_after zero accepted (V3)",
			msgs: []*Message{{
				Name:       "a",
				Keywords:   []string{"x"},
				Reasoning:  "legacy",
				StallAfter: toPtr(0),
			}},
			wantErr: "",
		},
		{
			name: "out-of-range stall_after rejected (V3)",
			msgs: []*Message{{
				Name:            "a",
				Keywords:        []string{"x"},
				ReasoningChunks: []string{"c0", "c1"},
				StallAfter:      toPtr(2),
			}},
			wantErr: "stall_after",
		},
		{
			name: "negative stall_after rejected (V3)",
			msgs: []*Message{{
				Name:            "a",
				Keywords:        []string{"x"},
				ReasoningChunks: []string{"c0", "c1"},
				StallAfter:      toPtr(-1),
			}},
			wantErr: "stall_after",
		},
		{
			name: "legacy single-reasoning stall_after beyond zero rejected (V3)",
			msgs: []*Message{{
				Name:       "a",
				Keywords:   []string{"x"},
				Reasoning:  "legacy",
				StallAfter: toPtr(1),
			}},
			wantErr: "stall_after",
		},
		{
			name: "stall_after without any reasoning still bounded to zero (V3)",
			msgs: []*Message{{
				Name:       "a",
				Keywords:   []string{"x"},
				StallAfter: toPtr(1),
			}},
			wantErr: "stall_after",
		},
		{
			name: "tool_call with stall_after rejected (V5)",
			msgs: []*Message{{
				Name:       "a",
				Keywords:   []string{"x"},
				StallAfter: toPtr(0),
				ToolCall:   &ToolCall{Name: "t"},
			}},
			wantErr: "tool_call",
		},
		{
			name: "legacy stall:true with explicit stall_after accepted (D3 precedence)",
			msgs: []*Message{{
				Name:            "a",
				Keywords:        []string{"x"},
				ReasoningChunks: []string{"c0", "c1"},
				Stall:           true,
				StallAfter:      toPtr(1),
			}},
			wantErr: "",
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

// toPtr returns a pointer to v, the standard Go idiom for taking the
// address of a literal in table-driven tests (style/golang.md §指针).
func toPtr(v int) *int {
	return &v
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
	if len(got) != 32 {
		t.Fatalf("NewMessageStore loaded %d messages, want 32 (agent-v2-fail + agent-v2-fail-mid + agent-v2-followup + agent-v2-greet + agent-v2-plain + agent-v2-saolei-nodesktop + agent-v2-saolei-progressive + agent-v2-saolei-start + agent-v2-slow + chat-only + farewell + greeting + mouse-trigger + saolei-remain + saolei-single-op + saolei-start + saolei-structural-stop + stall-mid-reasoning + team-planner-memory-snapshot + team-planner-opening + team-planner-review-continue + team-planner-review-stop + team-planner-user-reply + team-planner-wait + team-player-opening + team-player-resume-start + team-player-resume-stop + team-player-role-lock + team-player-user-intake + think-healthy-cadence + think-interrupt-gap + think-interrupt-stall)", len(got))
	}

	// Sorted alphabetically: agent-v2-fail before agent-v2-fail-mid before
	// agent-v2-followup before agent-v2-greet before agent-v2-plain before
	// agent-v2-saolei-nodesktop before agent-v2-saolei-progressive before
	// agent-v2-saolei-start before agent-v2-slow (the responses-only
	// family, specs/049-agent-v2-dsh-init/contracts/fake-responses-wire.md
	// §3) before chat-only before farewell before greeting before
	// mouse-trigger before saolei-remain before saolei-single-op before
	// saolei-start before saolei-structural-stop before stall-mid-reasoning
	// before the team entries (specs/059-agent-v2-team-mode/tasks.md T011 —
	// 's' < 't') before think-healthy-cadence before think-interrupt-gap
	// before think-interrupt-stall
	// ("agent-v2-fail" < "agent-v2-fail-mid" because the former is a
	// prefix of the latter; "agent-v2-fail-mid" < "agent-v2-followup"
	// because 'a' < 'o' at the first differing rune;
	// "agent-v2-plain" < "agent-v2-saolei-start" because 'p' < 's'; "agent-v2-saolei-nodesktop" <
	// "agent-v2-saolei-progressive" < "agent-v2-saolei-start" by the
	// trailing tokens 'n' < 'p' < 's'; "agent-v2-saolei-start" <
	// "agent-v2-slow" because
	// 'a' < 'l' at the fourth rune of the trailing token;
	// "farewell" < "greeting" because 'f' < 'g';
	// "mouse-trigger" < "saolei-remain" because 'm' < 's';
	// "saolei-single-op" < "saolei-start" because 'i' < 't'; "saolei-start"
	// < "saolei-structural-stop" because 'a' < 'r';
	// "saolei-structural-stop" < "stall-mid-reasoning" because 'o' < 't';
	// "stall-mid-reasoning" < "team-planner-memory-snapshot" because 's' < 't';
	// "team-planner-memory-snapshot" < "team-planner-opening" because 'm' <
	// 'o' (the snapshot entry shadows the opening entry by Name on the
	// specificity tie — see team_planner.yaml); "team-planner-*" <
	// "team-player-*" because 'n' < 'y' at the first
	// differing rune of the middle token; "team-planner-user-reply" <
	// "team-planner-wait" because 'u' < 'w', and "team-planner-wait" <
	// "team-player-opening" by the same middle-token rule;
	// "team-player-resume-stop" < "team-player-role-lock" because 'e' < 'o'
	// at the second rune of the trailing token, and "team-player-role-lock" <
	// "team-player-user-intake" because 'r' < 'u'; "team-player-*" <
	// "think-healthy-cadence" because
	// 'e' < 'h' at the second rune;
	// "stall-mid-reasoning" < "think-healthy-cadence" because 's' < 't';
	// "think-healthy-cadence" < "think-interrupt-gap" because 'h' < 'i';
	// "think-interrupt-gap" < "think-interrupt-stall" because 'g' < 's').
	wantNames := []string{
		"agent-v2-fail",
		"agent-v2-fail-mid",
		"agent-v2-followup",
		"agent-v2-greet",
		"agent-v2-plain",
		"agent-v2-saolei-nodesktop",
		"agent-v2-saolei-progressive",
		"agent-v2-saolei-start",
		"agent-v2-slow",
		"chat-only",
		"farewell",
		"greeting",
		"mouse-trigger",
		"saolei-remain",
		"saolei-single-op",
		"saolei-start",
		"saolei-structural-stop",
		"stall-mid-reasoning",
		"team-planner-memory-snapshot",
		"team-planner-opening",
		"team-planner-review-continue",
		"team-planner-review-stop",
		"team-planner-user-reply",
		"team-planner-wait",
		"team-player-opening",
		"team-player-resume-start",
		"team-player-resume-stop",
		"team-player-role-lock",
		"team-player-user-intake",
		"think-healthy-cadence",
		"think-interrupt-gap",
		"think-interrupt-stall",
	}
	for i, want := range wantNames {
		if got[i].Name != want {
			t.Fatalf("message[%d] = %q, want %q", i, got[i].Name, want)
		}
	}

	chatOnly := got[9]
	if chatOnly.Reasoning != "Responding with text only, no tools needed." {
		t.Errorf("chat-only reasoning = %q, want the no-tools reasoning", chatOnly.Reasoning)
	}
	if chatOnly.Text != "Sure, let's chat!" {
		t.Errorf("chat-only text = %q, want the chat text", chatOnly.Text)
	}
	if !slices.Contains(chatOnly.Keywords, "chat") {
		t.Errorf("chat-only keywords missing chat: %v", chatOnly.Keywords)
	}

	farewell := got[10]
	if farewell.Reasoning != "The user is saying goodbye." {
		t.Errorf("farewell reasoning = %q, want the goodbye reasoning", farewell.Reasoning)
	}
	if farewell.Text != "Goodbye! Have a great day!" {
		t.Errorf("farewell text = %q, want goodbye text", farewell.Text)
	}
	if !slices.Contains(farewell.Keywords, "bye") {
		t.Errorf("farewell keywords missing bye: %v", farewell.Keywords)
	}

	greeting := got[11]
	if greeting.Reasoning != "The user is greeting me, I should respond warmly." {
		t.Errorf("greeting reasoning = %q, want the warm greeting reasoning", greeting.Reasoning)
	}
	if greeting.Text != "Hello! How can I help you today?" {
		t.Errorf("greeting text = %q, want greeting text", greeting.Text)
	}
	if !slices.Contains(greeting.Keywords, "hello") {
		t.Errorf("greeting keywords missing hello: %v", greeting.Keywords)
	}

	// mouse-trigger carries a tool_call (the dispatch fix): a user turn
	// matching its keyword makes fake-LLM return a mouse_move tool_call
	// so the agent_operation large tests drive the real dispatch chain.
	mouseTrigger := got[12]
	if mouseTrigger.ToolCall == nil {
		t.Fatalf("mouse-trigger tool_call is nil")
	}
	if mouseTrigger.ToolCall.Name != "mouse_move" {
		t.Errorf("mouse-trigger tool_call.name = %q, want mouse_move", mouseTrigger.ToolCall.Name)
	}
	if !slices.Contains(mouseTrigger.Keywords, "move the mouse") {
		t.Errorf("mouse-trigger keywords missing 'move the mouse': %v", mouseTrigger.Keywords)
	}

	// saolei-remain carries a saolei_remain tool_call (spec 029 US2): a user
	// turn matching its keyword makes fake-LLM return a saolei_remain
	// tool_call so the agent_saolei large test drives the read-only remain
	// query end-to-end (specs/029-saolei-coord-remain/contracts/saolei-
	// remain-tool-contract.md §8).
	saoleiRemain := got[13]
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
	saoleiSingle := got[14]
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
	// point of the agent_saolei large-test flow). The "继续" keyword covers
	// a player turn whose last user message carries an appended continuation
	// instruction (spec 039 FR-017 semantics), keeping the multi-game flow
	// deterministic.
	saoleiStart := got[15]
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
	saoleiStructural := got[16]
	if saoleiStructural.ToolCall == nil {
		t.Fatalf("saolei-structural-stop tool_call is nil")
	}
	if saoleiStructural.ToolCall.Name != "saolei_operate" {
		t.Errorf("saolei-structural-stop tool_call.name = %q, want saolei_operate", saoleiStructural.ToolCall.Name)
	}
	if !slices.Contains(saoleiStructural.Keywords, "structural stop") {
		t.Errorf("saolei-structural-stop keywords missing 'structural stop': %v", saoleiStructural.Keywords)
	}

	// stall-mid-reasoning carries the stream-stall trigger (specs/043-llm-
	// stream-stall-recovery — the T011 large test): the streaming handler
	// emits the reasoning delta then blocks with the connection alive, so
	// the agent's idle timeout is the only way out. It must be flagged
	// Stall (not a plain text message) so the matcher excludes it from the
	// random fallback pool — an unrelated turn can never stall randomly.
	// The pinned reasoning/text/helpers constants live in helpers_test.go
	// (expectedStallReasoning — keep in sync).
	stallMidReasoning := got[17]
	if !stallMidReasoning.Stall {
		t.Errorf("stall-mid-reasoning must carry stall=true (the stream must pause after the first chunk)")
	}
	if stallMidReasoning.ToolCall != nil {
		t.Errorf("stall-mid-reasoning must NOT carry a tool_call (a stalled stream cannot return a tool call)")
	}
	if stallMidReasoning.Reasoning != "The user asked me to simulate a stream stall. I will send this reasoning chunk and then stop sending data while keeping the connection alive." {
		t.Errorf("stall-mid-reasoning reasoning = %q, want the pinned stall reasoning", stallMidReasoning.Reasoning)
	}
	if !slices.Contains(stallMidReasoning.Keywords, "stall now") {
		t.Errorf("stall-mid-reasoning keywords missing 'stall now': %v", stallMidReasoning.Keywords)
	}

	// The team entries (specs/059-agent-v2-team-mode/tasks.md T011/T018/T023)
	// serve the deterministic two-role team chain: the planner persona anchor
	// distinguishes them from every other family, the review entries carry
	// the task's terminal-result history condition, the LOSS review carries
	// the memory tool call whose result rule continues the review text
	// (T023), the snapshot entry carries the reloaded-memory system
	// condition, the wait entry carries the controllable in-flight window the
	// queue/cancel/refresh cases use, the player role-lock entry carries the
	// saolei guidance system condition, and the player entries carry the
	// saolei_init tool call / no-new-game text the drive script consumes. The
	// ordered block below is pinned against team_planner.yaml /
	// team_player.yaml (README.md §6 lockstep).
	teamPlannerMemorySnapshot := got[18]
	if !slices.Contains(teamPlannerMemorySnapshot.SystemKeywords, "长期记忆：") {
		t.Errorf("team-planner-memory-snapshot system_keywords missing the snapshot header: %v", teamPlannerMemorySnapshot.SystemKeywords)
	}
	if !slices.Contains(teamPlannerMemorySnapshot.SystemKeywords, "本局复盘观察：中心区域开局稳定") {
		t.Errorf("team-planner-memory-snapshot system_keywords missing the written observation line: %v", teamPlannerMemorySnapshot.SystemKeywords)
	}
	if teamPlannerMemorySnapshot.ToolCall != nil {
		t.Errorf("team-planner-memory-snapshot must carry a plain text response (the reload assertion body)")
	}

	teamPlannerOpening := got[19]
	if teamPlannerOpening.Name != "team-planner-opening" {
		t.Errorf("messages[19] name = %q, want team-planner-opening", teamPlannerOpening.Name)
	}
	if !slices.Contains(teamPlannerOpening.SystemKeywords, "你是扫雷 planner") {
		t.Errorf("team-planner-opening system_keywords missing the planner persona anchor: %v", teamPlannerOpening.SystemKeywords)
	}
	if !slices.Contains(teamPlannerOpening.SystemKeywords, "扫雷玩法") {
		t.Errorf("team-planner-opening system_keywords missing the saolei:game section anchor: %v", teamPlannerOpening.SystemKeywords)
	}
	if teamPlannerOpening.ToolCall != nil {
		t.Errorf("team-planner-opening must carry a plain text response (the opening strategy body)")
	}
	if !strings.Contains(teamPlannerOpening.Text, "以下开局计划") {
		t.Errorf("team-planner-opening text = %q, want the player-side opening anchor", teamPlannerOpening.Text)
	}

	teamPlannerReviewContinue := got[20]
	if !slices.Contains(teamPlannerReviewContinue.Keywords, "<player-message>") {
		t.Errorf("team-planner-review-continue keywords missing the player broadcast marker: %v", teamPlannerReviewContinue.Keywords)
	}
	if !slices.Contains(teamPlannerReviewContinue.HistoryKeywords, "game status: won") {
		t.Errorf("team-planner-review-continue history_keywords missing the won terminal result: %v", teamPlannerReviewContinue.HistoryKeywords)
	}
	if teamPlannerReviewContinue.MinTurn != 2 {
		t.Errorf("team-planner-review-continue min_turn = %d, want 2 (off the first drive)", teamPlannerReviewContinue.MinTurn)
	}
	if !strings.Contains(teamPlannerReviewContinue.Text, "开始下一局") {
		t.Errorf("team-planner-review-continue text = %q, want the continue-next-game instruction", teamPlannerReviewContinue.Text)
	}

	teamPlannerReviewStop := got[21]
	if !slices.Contains(teamPlannerReviewStop.HistoryKeywords, "game status: lost") {
		t.Errorf("team-planner-review-stop history_keywords missing the lost terminal result: %v", teamPlannerReviewStop.HistoryKeywords)
	}
	if teamPlannerReviewStop.MinTurn != 2 {
		t.Errorf("team-planner-review-stop min_turn = %d, want 2 (off the first drive)", teamPlannerReviewStop.MinTurn)
	}
	// The loss review writes its fixed cross-game observation through the
	// memory tool first; the review text arrives via the tool-result rule in
	// agent_v2_saolei_tools.yaml (T023 persistence path).
	if teamPlannerReviewStop.ToolCall == nil || teamPlannerReviewStop.ToolCall.Name != "memory" {
		t.Errorf("team-planner-review-stop tool_call = %+v, want the memory add", teamPlannerReviewStop.ToolCall)
	} else if content, _ := teamPlannerReviewStop.ToolCall.Arguments["content"].(string); !strings.Contains(content, "本局复盘观察") {
		t.Errorf("team-planner-review-stop memory content = %q, want the fixed observation", content)
	}
	if strings.Contains(teamPlannerReviewStop.Text, "开始下一局") {
		t.Errorf("team-planner-review-stop text = %q, must not carry the next-game instruction", teamPlannerReviewStop.Text)
	}

	teamPlannerUserReply := got[22]
	if !slices.Contains(teamPlannerUserReply.Keywords, "暂停") {
		t.Errorf("team-planner-user-reply keywords missing the queued-user anchor: %v", teamPlannerUserReply.Keywords)
	}
	if teamPlannerUserReply.MinTurn != 2 {
		t.Errorf("team-planner-user-reply min_turn = %d, want 2 (off the first drive)", teamPlannerUserReply.MinTurn)
	}

	teamPlannerWait := got[23]
	if !slices.Contains(teamPlannerWait.Keywords, "planner-wait") {
		t.Errorf("team-planner-wait keywords missing the controllable-window anchor: %v", teamPlannerWait.Keywords)
	}
	if len(teamPlannerWait.ReasoningChunks) != 2 {
		t.Errorf("team-planner-wait reasoning_chunks = %v, want 2 chunks (the inter-chunk window)", teamPlannerWait.ReasoningChunks)
	}
	if !slices.Equal(teamPlannerWait.ChunkDelays, []string{"4s"}) {
		t.Errorf("team-planner-wait chunk_delays = %v, want [4s] (the queue/cancel/refresh window)", teamPlannerWait.ChunkDelays)
	}
	if teamPlannerWait.ToolCall != nil {
		t.Errorf("team-planner-wait must carry a plain text response (the long-running planner turn)")
	}

	teamPlayerOpening := got[24]
	if !slices.Contains(teamPlayerOpening.SystemKeywords, "你是扫雷 player") {
		t.Errorf("team-player-opening system_keywords missing the player persona anchor: %v", teamPlayerOpening.SystemKeywords)
	}
	if teamPlayerOpening.ToolCall == nil || teamPlayerOpening.ToolCall.Name != "saolei_init" {
		t.Errorf("team-player-opening tool_call = %+v, want saolei_init", teamPlayerOpening.ToolCall)
	}

	teamPlayerResumeStart := got[25]
	if teamPlayerResumeStart.ToolCall == nil || teamPlayerResumeStart.ToolCall.Name != "saolei_init" {
		t.Errorf("team-player-resume-start tool_call = %+v, want saolei_init", teamPlayerResumeStart.ToolCall)
	}
	if teamPlayerResumeStart.MinTurn != 2 {
		t.Errorf("team-player-resume-start min_turn = %d, want 2 (off the first drive)", teamPlayerResumeStart.MinTurn)
	}

	teamPlayerResumeStop := got[26]
	if teamPlayerResumeStop.ToolCall != nil {
		t.Errorf("team-player-resume-stop must carry a plain text response (no new game opened)")
	}
	if !strings.Contains(teamPlayerResumeStop.Text, "本局到此") {
		t.Errorf("team-player-resume-stop text = %q, want the stop acknowledgement", teamPlayerResumeStop.Text)
	}
	if teamPlayerResumeStop.MinTurn != 2 {
		t.Errorf("team-player-resume-stop min_turn = %d, want 2 (off the first drive)", teamPlayerResumeStop.MinTurn)
	}

	teamPlayerRoleLock := got[27]
	if !slices.Contains(teamPlayerRoleLock.SystemKeywords, "## saolei (Minesweeper tools)") {
		t.Errorf("team-player-role-lock system_keywords missing the saolei guidance heading: %v", teamPlayerRoleLock.SystemKeywords)
	}
	if teamPlayerRoleLock.ToolCall != nil {
		t.Errorf("team-player-role-lock must carry a plain text response (the guidance assertion body)")
	}

	teamPlayerUserIntake := got[28]
	if teamPlayerUserIntake.ToolCall != nil {
		t.Errorf("team-player-user-intake must carry a plain text response (the queued-message digest)")
	}
	if !slices.Contains(teamPlayerUserIntake.Keywords, "继续") {
		t.Errorf("team-player-user-intake keywords missing the queued-user anchor: %v", teamPlayerUserIntake.Keywords)
	}
	if teamPlayerUserIntake.MinTurn != 2 {
		t.Errorf("team-player-user-intake min_turn = %d, want 2 (off the first drive)", teamPlayerUserIntake.MinTurn)
	}

	// The three think-interrupt demonstration templates (specs/046-fake-llm-
	// think-chunking — contract specs/046-fake-llm-think-chunking/contracts/
	// template-config.md §3.3-§3.5, added in T012): they exercise the
	// chunked-reasoning / chunk_delays / stall_after fields end-to-end in the
	// embedded store, and each is excluded from the no-match random fallback
	// pool by isHangCapable (FR-011).
	thinkHealthy := got[29]
	if len(thinkHealthy.ReasoningChunks) != 3 {
		t.Errorf("think-healthy-cadence reasoning_chunks = %v, want 3 chunks", thinkHealthy.ReasoningChunks)
	}
	if thinkHealthy.ReasoningChunks[0] != "Step one." || thinkHealthy.ReasoningChunks[2] != "Step three." {
		t.Errorf("think-healthy-cadence reasoning_chunks = %v, want the healthy-cadence chunks", thinkHealthy.ReasoningChunks)
	}
	if !slices.Equal(thinkHealthy.ChunkDelays, []string{"200ms", "200ms"}) {
		t.Errorf("think-healthy-cadence chunk_delays = %v, want [200ms 200ms]", thinkHealthy.ChunkDelays)
	}
	if thinkHealthy.StallAfter != nil {
		t.Errorf("think-healthy-cadence stall_after = %v, want nil (no stall)", *thinkHealthy.StallAfter)
	}
	if thinkHealthy.Text != "Done." {
		t.Errorf("think-healthy-cadence text = %q, want 'Done.'", thinkHealthy.Text)
	}

	thinkGap := got[30]
	if len(thinkGap.ReasoningChunks) != 3 {
		t.Errorf("think-interrupt-gap reasoning_chunks = %v, want 3 chunks", thinkGap.ReasoningChunks)
	}
	if thinkGap.ReasoningChunks[0] != "Analyzing the board state." || thinkGap.ReasoningChunks[2] != "Finalizing the safest move." {
		t.Errorf("think-interrupt-gap reasoning_chunks = %v, want the board-analysis chunks", thinkGap.ReasoningChunks)
	}
	if !slices.Equal(thinkGap.ChunkDelays, []string{"1s", "15s"}) {
		t.Errorf("think-interrupt-gap chunk_delays = %v, want [1s 15s]", thinkGap.ChunkDelays)
	}
	if thinkGap.StallAfter != nil {
		t.Errorf("think-interrupt-gap stall_after = %v, want nil (long finite gap, not a stall)", *thinkGap.StallAfter)
	}
	if thinkGap.Text != "Placing the flag at (3,4)." {
		t.Errorf("think-interrupt-gap text = %q, want 'Placing the flag at (3,4).'", thinkGap.Text)
	}

	thinkStall := got[31]
	if len(thinkStall.ReasoningChunks) != 2 {
		t.Errorf("think-interrupt-stall reasoning_chunks = %v, want 2 chunks", thinkStall.ReasoningChunks)
	}
	if thinkStall.ReasoningChunks[0] != "Starting to reason about the request." || thinkStall.ReasoningChunks[1] != "Going deeper into analysis." {
		t.Errorf("think-interrupt-stall reasoning_chunks = %v, want the analysis chunks", thinkStall.ReasoningChunks)
	}
	if !slices.Equal(thinkStall.ChunkDelays, []string{"1s"}) {
		t.Errorf("think-interrupt-stall chunk_delays = %v, want [1s]", thinkStall.ChunkDelays)
	}
	if thinkStall.StallAfter == nil || *thinkStall.StallAfter != 1 {
		t.Errorf("think-interrupt-stall stall_after = %v, want 1 (block after the second chunk)", thinkStall.StallAfter)
	}
	if thinkStall.Text != "This answer never arrives." {
		t.Errorf("think-interrupt-stall text = %q, want 'This answer never arrives.'", thinkStall.Text)
	}
}

// TestNewMessageStore_LoadsEmbeddedTools verifies the embedded
// tool-config files (operation_tools.yaml and saolei_tools.yaml, grouped by
// module per specs/046-fake-llm-think-chunking/data-model.md §7) are parsed
// into the store's Tools slice with the configured values, and sorted
// alphabetically by Name.
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
	if len(tools) != 18 {
		t.Fatalf("NewMessageStore loaded %d tools, want 18 (agent-v2-saolei-init-lost, agent-v2-saolei-init-nodesktop, agent-v2-saolei-init-operate, agent-v2-saolei-init-operate-progressive, agent-v2-saolei-operate-final, agent-v2-saolei-operate-lost, agent-v2-saolei-operate-nodesktop, agent-v2-saolei-operate-won, keyboard-success-text, mouse-click-button, mouse-click-success-text, mouse-move-followup-click, mouse-move-oob, mouse-move-success-text, saolei-init-followup-operate, saolei-operate-final-text, saolei-remain-final-text, team-planner-review-stop-text)", len(tools))
	}

	// Sorted alphabetically by Name.
	wantNames := []string{
		"agent-v2-saolei-init-lost",
		"agent-v2-saolei-init-nodesktop",
		"agent-v2-saolei-init-operate",
		"agent-v2-saolei-init-operate-progressive",
		"agent-v2-saolei-operate-final",
		"agent-v2-saolei-operate-lost",
		"agent-v2-saolei-operate-nodesktop",
		"agent-v2-saolei-operate-won",
		"keyboard-success-text",
		"mouse-click-button",
		"mouse-click-success-text",
		"mouse-move-followup-click",
		"mouse-move-oob",
		"mouse-move-success-text",
		"saolei-init-followup-operate",
		"saolei-operate-final-text",
		"saolei-remain-final-text",
		"team-planner-review-stop-text",
	}
	for i, want := range wantNames {
		if tools[i].Name != want {
			t.Errorf("tools[%d] name = %q, want %q", i, tools[i].Name, want)
		}
	}

	// mouse-click-button produces a LEFT_CLICK mouse_click tool_call when
	// the result text contains "click here". After the US2 split a click
	// carries only click_type (no coordinates).
	clickButton := tools[9]
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
	clickSuccess := tools[10]
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
	moveFollowup := tools[11]
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
	moveOob := tools[12]
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
	moveSuccess := tools[13]
	if moveSuccess.ToolName != "mouse_move" {
		t.Errorf("mouse-move-success-text tool_name = %q, want mouse_move", moveSuccess.ToolName)
	}
	if moveSuccess.RespondWith.Text != "I see the screen now." {
		t.Errorf("mouse-move-success-text respond_with.text = %q, want 'I see the screen now.'", moveSuccess.RespondWith.Text)
	}
	if moveSuccess.RespondWith.ToolCall != nil {
		t.Errorf("mouse-move-success-text respond_with.tool_call should be nil")
	}

	// saolei-init-followup-operate chains a saolei_init result into a
	// saolei_operate BATCH tool_call (operations: [click{3,4}, click{5,6}])
	// — spec 039-planner-memory-calibration US1 (FR-001/FR-002): the merged
	// dual-form tool executes both ops IN ORDER in one call and returns once.
	saoleiInitOperate := tools[14]
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
	saoleiOperateFinal := tools[15]
	if saoleiOperateFinal.Name != "saolei-operate-final-text" {
		t.Errorf("tools[15] name = %q, want saolei-operate-final-text", saoleiOperateFinal.Name)
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
	saoleiRemainFinal := tools[16]
	if saoleiRemainFinal.Name != "saolei-remain-final-text" {
		t.Errorf("tools[16] name = %q, want saolei-remain-final-text", saoleiRemainFinal.Name)
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

	// team-planner-review-stop-text continues the lost-game review after its
	// memory add lands (T023): tool_name=memory matches the SUT's memory tool
	// result ("memory added") and supplies the review body the team memory
	// large test asserts.
	teamPlannerReviewStopText := tools[17]
	if teamPlannerReviewStopText.Name != "team-planner-review-stop-text" {
		t.Errorf("tools[17] name = %q, want team-planner-review-stop-text", teamPlannerReviewStopText.Name)
	}
	if teamPlannerReviewStopText.ToolName != "memory" {
		t.Errorf("team-planner-review-stop-text tool_name = %q, want memory", teamPlannerReviewStopText.ToolName)
	}
	if !slices.Contains(teamPlannerReviewStopText.MatchResultContains, "memory added") {
		t.Errorf("team-planner-review-stop-text match_result_contains missing 'memory added': %v", teamPlannerReviewStopText.MatchResultContains)
	}
	if !strings.Contains(teamPlannerReviewStopText.RespondWith.Text, "本局到此") || strings.Contains(teamPlannerReviewStopText.RespondWith.Text, "开始下一局") {
		t.Errorf("team-planner-review-stop-text respond_with.text = %q, want the stop wording and no next-game instruction", teamPlannerReviewStopText.RespondWith.Text)
	}
	if teamPlannerReviewStopText.RespondWith.ToolCall != nil {
		t.Errorf("team-planner-review-stop-text respond_with.tool_call should be nil")
	}
}
