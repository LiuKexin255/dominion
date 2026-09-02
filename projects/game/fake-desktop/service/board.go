// Package service implements the fake-desktop executor: a deterministic
// test-infrastructure desktop stand-in (spec
// specs/051-agent-v2-dsh-migration/research.md D15, spec A9) that connects to
// the game gateway /api/v2 WebSocket flow entry, executes received FlowPart
// operations against a fixed board model, and replies with pre-generated,
// recognizer-verifiable screenshots.
package service

import (
	"fmt"
)

// opClick/opFlag/opChord name the cell-operation kinds a ScenarioStep
// consumes (the MouseClickAction subset dispatched by the saolei cell
// operations).
const (
	opClick = "click"
	opFlag  = "flag"
	opChord = "chord"
)

// ScenarioStep is one transition of a deterministic game: consuming the next
// cell operation of the given kind advances the model and the board image
// becomes png.
type ScenarioStep struct {
	// Op is the operation kind this step consumes: click, flag, or chord.
	Op string
	// PNG is the board screenshot after the operation lands.
	PNG []byte
}

// Scenario is one deterministic game as a fixed image sequence: the screenshot
// returned for the F2 new-game keypress plus the per-operation transitions.
// Determinism comes from the fixed mapping — the same operation sequence
// always observes the same screenshots (research.md D15: 图集映射优先于运行时
// 渲染).
type Scenario struct {
	// Name identifies the scenario in logs and the FAKE_DESKTOP_SCENARIO
	// selector.
	Name string
	// InitPNG is returned for the F2 new-game keypress.
	InitPNG []byte
	// Steps are consumed in order; an operation that does not match the
	// pending step's kind leaves the board unchanged (idempotent re-read —
	// the recognized state must stay compatible across screenshots of one
	// game, so repeating the current image is always safe).
	Steps []ScenarioStep
}

// board is the per-connection deterministic model: a scenario plus the cursor
// into its step list.
type board struct {
	scenario *Scenario
	step     int
}

// newBoard starts a fresh game on the scenario.
func newBoard(scenario *Scenario) *board {
	return &board{scenario: scenario}
}

// newGame handles the F2 keypress: the model restarts and the scenario's
// initial board is returned.
func (b *board) newGame() []byte {
	b.step = 0
	return b.scenario.InitPNG
}

// applyCellOp consumes one cell operation: when it matches the pending step's
// kind the model advances and the post-operation image is returned; otherwise
// the current image is repeated. Advancing past the last step keeps returning
// the final image (a terminal board is stable — every later screenshot of the
// same game is identical).
func (b *board) applyCellOp(op string) ([]byte, error) {
	if op != opClick && op != opFlag && op != opChord {
		return nil, fmt.Errorf("unknown cell op %q", op)
	}
	if b.step < len(b.scenario.Steps) && b.scenario.Steps[b.step].Op == op {
		png := b.scenario.Steps[b.step].PNG
		b.step++
		return png, nil
	}
	return b.current(), nil
}

// current returns the board image for the model's present state.
func (b *board) current() []byte {
	if b.step == 0 {
		return b.scenario.InitPNG
	}
	return b.scenario.Steps[b.step-1].PNG
}
