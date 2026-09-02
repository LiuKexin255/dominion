package service

import (
	"embed"
	"fmt"
)

// The embedded screenshots are cmp-identical copies of the authoritative
// @dominion/game-saolei-board golden testdata (projects/game/pkg/saolei-board/
// testdata, golden-verified by golden.test.ts) — the same fixture-reuse
// precedent as projects/game/testplan/testdata. The agent side runs the REAL
// recognizer in large tests, so every screenshot the executor returns MUST be
// a recognizable Minesweeper board of the same game (dimensions fixed at init,
// revealed cells never revert — projects/game/pkg/saolei-board/src/core/
// recognize.ts updateFromScreenshot). Scenario transitions therefore only
// move forward within one fixture family:
//
//	progressive: saolei_1 (16×16 all INITIAL) → saolei_3 (partially revealed)
//	             → saolei_4 (saolei_3 plus flags)
//	won:         saolei_10 (9×9 win board, counter 000)
//	lost:        saolei_5 (16×16 loss board with HIT_MINE/MINE)
//
//go:embed testdata/*.png
var fixtureFS embed.FS

// fixture reads one embedded screenshot.
func fixture(name string) ([]byte, error) {
	data, err := fixtureFS.ReadFile("testdata/" + name)
	if err != nil {
		return nil, fmt.Errorf("load fixture %s: %w", name, err)
	}
	return data, nil
}

// scenarioNames lists the selectable scenarios (FAKE_DESKTOP_SCENARIO).
const (
	scenarioWon         = "won"
	scenarioLost        = "lost"
	scenarioProgressive = "progressive"
)

// loadScenario assembles the named scenario from the embedded fixtures.
func loadScenario(name string) (*Scenario, error) {
	switch name {
	case "", scenarioWon:
		// A finished win board: the F2 reply already carries `game
		// status: won`, and every cell operation is rejected agent-side
		// before dispatch, so the scenario has no cell transitions.
		png, err := fixture("saolei_10.png")
		if err != nil {
			return nil, err
		}
		return &Scenario{Name: scenarioWon, InitPNG: png}, nil
	case scenarioLost:
		png, err := fixture("saolei_5.png")
		if err != nil {
			return nil, err
		}
		return &Scenario{Name: scenarioLost, InitPNG: png}, nil
	case scenarioProgressive:
		initPNG, err := fixture("saolei_1.png")
		if err != nil {
			return nil, err
		}
		clickedPNG, err := fixture("saolei_3.png")
		if err != nil {
			return nil, err
		}
		flaggedPNG, err := fixture("saolei_4.png")
		if err != nil {
			return nil, err
		}
		return &Scenario{
			Name:    scenarioProgressive,
			InitPNG: initPNG,
			Steps: []ScenarioStep{
				{Op: opClick, PNG: clickedPNG},
				{Op: opFlag, PNG: flaggedPNG},
			},
		}, nil
	default:
		return nil, fmt.Errorf("unknown scenario %q (want %s | %s | %s)",
			name, scenarioWon, scenarioLost, scenarioProgressive)
	}
}
