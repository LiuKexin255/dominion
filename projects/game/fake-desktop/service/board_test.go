package service

import (
	"bytes"
	"image/png"
	"testing"
)

// testScenario assembles a scenario from in-memory PNG bytes so the model
// tests do not depend on the embedded fixtures.
func testScenario(t *testing.T) *Scenario {
	t.Helper()
	return &Scenario{
		Name:    "test",
		InitPNG: []byte("init-png"),
		Steps: []ScenarioStep{
			{Op: opClick, PNG: []byte("clicked-png")},
			{Op: opFlag, PNG: []byte("flagged-png")},
		},
	}
}

func TestBoardNewGameReturnsInitAndResets(t *testing.T) {
	scenario := testScenario(t)
	b := newBoard(scenario)

	if got := b.newGame(); string(got) != "init-png" {
		t.Fatalf("newGame() = %q, want init-png", got)
	}
	// Advance past both steps, then restart: the model must be back at the
	// beginning (F2 re-seeds the game).
	if _, err := b.applyCellOp(opClick); err != nil {
		t.Fatalf("applyCellOp(click): %v", err)
	}
	if _, err := b.applyCellOp(opFlag); err != nil {
		t.Fatalf("applyCellOp(flag): %v", err)
	}
	if got := string(b.current()); got != "flagged-png" {
		t.Fatalf("current() = %q, want flagged-png", got)
	}
	if got := b.newGame(); string(got) != "init-png" {
		t.Fatalf("newGame() after restart = %q, want init-png", got)
	}
	if got := string(b.current()); got != "init-png" {
		t.Fatalf("current() after restart = %q, want init-png", got)
	}
}

func TestBoardApplyCellOpTransitions(t *testing.T) {
	tests := []struct {
		name    string
		ops     []string
		wantPNG []string
	}{
		{
			name:    "matching ops advance the model",
			ops:     []string{opClick, opFlag},
			wantPNG: []string{"clicked-png", "flagged-png"},
		},
		{
			name:    "non-matching op repeats the current image",
			ops:     []string{opFlag, opClick},
			wantPNG: []string{"init-png", "clicked-png"},
		},
		{
			name:    "terminal board is stable",
			ops:     []string{opClick, opFlag, opClick, opClick},
			wantPNG: []string{"clicked-png", "flagged-png", "flagged-png", "flagged-png"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newBoard(testScenario(t))
			b.newGame()
			for i, op := range tt.ops {
				got, err := b.applyCellOp(op)
				if err != nil {
					t.Fatalf("applyCellOp(%q): %v", op, err)
				}
				if string(got) != tt.wantPNG[i] {
					t.Fatalf("op %d (%s) = %q, want %q", i, op, got, tt.wantPNG[i])
				}
			}
		})
	}
}

func TestBoardApplyCellOpUnknownKind(t *testing.T) {
	b := newBoard(testScenario(t))
	if _, err := b.applyCellOp("zoom"); err == nil {
		t.Fatal("applyCellOp(zoom) expected an error")
	}
}

func TestLoadScenarioFixturesArePNG(t *testing.T) {
	for _, name := range []string{"", scenarioWon, scenarioLost, scenarioProgressive} {
		t.Run(name, func(t *testing.T) {
			s, err := loadScenario(name)
			if err != nil {
				t.Fatalf("loadScenario(%q): %v", name, err)
			}
			assertPNG(t, s.InitPNG)
			for i, step := range s.Steps {
				assertPNG(t, step.PNG)
				if step.Op != opClick && step.Op != opFlag && step.Op != opChord {
					t.Fatalf("step %d has unknown op %q", i, step.Op)
				}
			}
		})
	}
}

func TestLoadScenarioUnknownName(t *testing.T) {
	if _, err := loadScenario("chaos"); err == nil {
		t.Fatal("loadScenario(chaos) expected an error")
	}
}

// assertPNG decodes the image to prove the fixture is a well-formed PNG.
func assertPNG(t *testing.T, data []byte) {
	t.Helper()
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("png.DecodeConfig: %v", err)
	}
	if cfg.Width == 0 || cfg.Height == 0 {
		t.Fatalf("degenerate png dimensions %dx%d", cfg.Width, cfg.Height)
	}
}
