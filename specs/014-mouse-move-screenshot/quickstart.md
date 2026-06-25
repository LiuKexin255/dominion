# Quickstart: Mouse Move Action & Post-Operation Screenshot Feedback

**Feature**: 014-mouse-move-screenshot
**Date**: 2026-06-25

## Prerequisites

- Windows machine with a visible application window (e.g., Notepad, Calculator)
- Desktop binary built: `bazel build --platforms=@rules_go//go/toolchain:windows_amd64 //projects/game/desktop:desktop_lib`
- Agent service tests pass: `bazel test //projects/game/agent:lib_test`
- Desktop Go tests pass: `bazel test //projects/game/desktop:desktop_test`

## Validation Scenarios

### Scenario 1: MOVE action produces no button events

**Test command**:
```bash
bazel test //projects/game/desktop:desktop_test -- --test_filter=Test_actionEventSequence_Move
```

**Expected outcome**: `actionEventSequence(MOVE)` returns an empty `[]uint32{}`. `validateMouseAction(MOVE)` returns `nil` (valid).

### Scenario 2: Post-operation screenshot includes overlay marker

**Test command**:
```bash
bazel test //projects/game/desktop:desktop_test -- --test_filter=TestOverlayMarker
```

**Expected outcome**: A marker function draws a red ring at the specified coordinate on a test image. The output image has red pixels at distance `r` from center `(x, y)`.

### Scenario 3: Mouse tool returns image content blocks

**Test command**:
```bash
bazel test //projects/game/agent:lib_test -- --test_filter=screenshot
```

**Expected outcome**: When the bridge returns a result with screenshot data, the mouse tool callback returns a content-block array `[text, image_url, text]` where the last text block contains the pixel-dimension annotation.

### Scenario 4: MOVE without bound window fails

**Test command**:
```bash
bazel test //projects/game/desktop:desktop_test -- --test_filter=TestExecuteAgentOperation_MoveNoWindow
```

**Expected outcome**: MOVE action with `boundWin.Handle == 0` returns `FAILED` with message `"no window bound"`.

### Scenario 5: Full end-to-end on Windows

**Manual steps**:
1. Launch the desktop binary, bind a visible window (e.g., Calculator).
2. Start a chat session with an agent that has the `mouse` tool.
3. Ask the agent to "click the '7' button".
4. Observe: the agent issues a mouse tool call, the result includes a screenshot with a red marker at the click position, and the agent describes the post-action screen state.
5. Ask the agent to "move the cursor to the top-left corner".
6. Observe: the agent issues a MOVE action, no click occurs, and the result screenshot shows the marker at the new position.

**Expected outcome**: The agent receives visual feedback after each operation and can self-correct coordinates.
