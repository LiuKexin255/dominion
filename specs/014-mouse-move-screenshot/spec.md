# Feature Specification: Mouse Move Action & Post-Operation Screenshot Feedback

**Feature Branch**: `014-mouse-move-screenshot`

**Created**: 2026-06-25

**Status**: Draft

**Input**: User description: "现在 desktop 可以正常鼠标操作，但 llm 无法正确识别位置，所以新增如下功能：1. mouse 工具增加 move 操作，仅移动鼠标，用于定位 2. mouse tool 执行结果包括窗口截图（如果窗口选框不为空），让 agent 可以根据截图进行调整。"

## Clarifications

### Session 2026-06-25

- Q: When no target window is selected/bound, how should the mouse MOVE action behave? → A: Fail like clicks.
- Q: Should post-operation screenshots visually show the cursor/target position after a mouse operation? → A: Overlay marker.
- Q: Should the image size-annotation text block apply to all images the agent sees, including tool result screenshots? → A: Yes, universally.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Post-Operation Screenshot Feedback (Priority: P1)

When the agent executes a mouse operation (click, double-click, etc.), the desktop captures a fresh screenshot of the bound window and returns it alongside the operation result. The agent sees the post-action state of the screen — where the cursor landed, whether a menu opened, whether a button was pressed — and can decide whether to continue, retry, or adjust its next action.

**Why this priority**: The agent currently operates blind after each action. It receives only a text status ("ok" / "failed") with no visual confirmation. This makes it impossible for the agent to self-correct when its coordinate estimate is wrong — a problem the user has explicitly identified as the root cause of inaccurate operations. Visual feedback closes the perception-action loop and is the single highest-leverage improvement for positioning accuracy.

**Independent Test**: The agent issues a mouse click, the desktop returns a screenshot in the result, and the agent can describe what it sees on screen in its next response — demonstrating it received and interpreted the post-action visual state.

**Acceptance Scenarios**:

1. **Given** a window is bound to the desktop, **When** the agent issues a LEFT_CLICK operation, **Then** the operation result carries a screenshot of the bound window captured immediately after the action executed, the screenshot visually marks the executed coordinate, and the agent receives it as part of the tool result.
2. **Given** no window is bound to the desktop, **When** the agent issues any mouse operation, **Then** the operation result does not carry a screenshot (the agent still receives the text status), and the result message explains that no window was bound.
3. **Given** a window is bound but the screenshot capture fails (e.g., the window was closed mid-operation), **When** the agent issues a mouse operation, **Then** the mouse action still executes, the operation result reports SUCCEEDED for the action itself, and the result includes an explanatory message about the screenshot failure rather than a screenshot.
4. **Given** a post-operation screenshot is returned, **When** the agent inspects the tool result, **Then** the screenshot's pixel dimensions are available alongside the image data so the agent can calibrate subsequent coordinates against the correct pixel space.

---

### User Story 2 - Mouse Move Action (Priority: P2)

The agent can issue a MOVE operation that repositions the cursor to a target position without pressing any button. This is useful for positioning the cursor before a subsequent click, for probing where a UI element is, or for hover interactions.

**Why this priority**: MOVE gives the agent a non-destructive way to explore the screen — it can reposition and observe the result via the post-action screenshot without committing to a click. This is secondary to the screenshot feedback loop (US1) because without visual feedback, moving the cursor provides no value to the agent.

**Independent Test**: The agent issues a MOVE operation, the cursor visibly moves to the specified position, and the operation result reports success. If a window is bound, a screenshot is returned with a visible marker at the executed position.

**Acceptance Scenarios**:

1. **Given** a window is bound, **When** the agent issues a MOVE operation to coordinates (x, y), **Then** the cursor moves to the screen-absolute position corresponding to those screenshot-relative coordinates, no button events are dispatched, and the operation result includes a post-action screenshot with a visible marker at (x, y).
2. **Given** no window is bound, **When** the agent issues a MOVE operation, **Then** the operation fails with the same no-window-bound behavior as click actions, no cursor movement occurs, and no screenshot is included in the result.
3. **Given** the agent issues a MOVE operation, **When** the coordinates fall outside the virtual screen bounds, **Then** the operation fails with a bounds-validation error and no cursor movement occurs.

---

### Edge Cases

- What happens when the mouse action fails but the screenshot capture succeeds? The result should report FAILED for the action and include the screenshot so the agent can see why the action failed (e.g., the window is in an unexpected state).
- What happens when the bound window is minimized between the action and the screenshot? The action may succeed (mouse events are dispatched regardless of window state), but the screenshot capture may fail or produce a zero-size image; the result should carry an appropriate message.
- What happens when the screenshot is very large (e.g., a 4K window)? The screenshot should be subject to the same size limit that applies to user-turn screenshots (5 MiB), and if it exceeds the limit, the result should carry a message rather than the image.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The mouse tool MUST support a new `MOVE` action that repositions the cursor to the specified image-relative coordinates without pressing any button.
- **FR-002**: The `MOVE` action MUST be available to the agent as a distinct action value in the tool schema, separate from all click-based actions.
- **FR-003**: The `MOVE` action MUST validate coordinates against the virtual screen bounds and fail without moving the cursor if the coordinates are out of bounds.
- **FR-004**: The `MOVE` action MUST require a bound window and MUST fail with the same no-window-bound behavior as click-based mouse actions when no window is selected.
- **FR-005**: After executing any mouse operation (including MOVE), if a window is bound, the desktop MUST capture a screenshot of the bound window and include it in the operation result.
- **FR-006**: If no window is bound, the operation result MUST NOT include a screenshot; the agent receives only the text status and message.
- **FR-007**: If the mouse action itself fails, the desktop MUST still attempt to capture a screenshot (when a window is bound) and include it in the result so the agent can see the post-failure screen state.
- **FR-008**: If the screenshot capture fails after a successful action, the result MUST report SUCCEEDED for the action and include an explanatory message about the screenshot failure.
- **FR-009**: The operation result MUST carry the screenshot's pixel dimensions (width and height in pixels) alongside the image data so the agent can calibrate subsequent coordinates.
- **FR-010**: The screenshot included in the operation result MUST be subject to the same maximum size constraint as user-turn screenshots; if it exceeds the limit, the result MUST carry an explanatory message rather than the image.
- **FR-011**: The mouse tool, when invoked, MUST return both a text status and any screenshot image data to the agent so the agent receives visual feedback as part of its tool-call result.
- **FR-012**: The mouse tool description presented to the agent MUST be updated to reflect the new MOVE action and the fact that results now include post-action screenshots.
- **FR-013**: When a post-operation screenshot is included, it MUST visually mark the executed coordinate so the agent can identify the action location even if the underlying screenshot capture does not include the OS cursor.
- **FR-014**: All image content delivered to the agent — whether in user-turn messages or tool-call results — MUST be accompanied by a text block annotating the image's pixel dimensions (width and height), so the agent can consistently calibrate coordinates against the correct pixel space regardless of image source.

### Key Entities *(include if feature involves data)*

- **AgentMouseAction (extended)**: The proto enum of supported mouse actions, extended with a new `MOVE` value representing cursor repositioning without button events.
- **AgentOperationResultFrame (extended)**: The operation result frame, extended to carry an optional screenshot (image data + pixel dimensions) alongside the existing operation_id, status, and message fields.
- **Post-Operation Marker**: A visible marker applied to a returned screenshot at the executed mouse coordinate, allowing the agent to compare intended and observed positions.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: After implementing the post-operation screenshot, the agent can correctly describe the on-screen state immediately following a mouse operation in 100% of test cases where a window is bound, demonstrating it received and interpreted the post-action visual feedback.
- **SC-002**: The agent can use the MOVE action to reposition the cursor without side effects in 100% of test cases, with no spurious click events dispatched.
- **SC-003**: The agent's mouse-operation accuracy (landing at the intended target) improves measurably after both features are implemented, because the agent can self-correct using post-action screenshots instead of operating blind.
- **SC-004**: The post-operation screenshot is captured and delivered within a perceptually-imperceptible delay (< 500 ms from action completion to screenshot delivery), so the agent's reasoning loop is not stalled.

## Assumptions

- The existing window-screenshot capture capability (`capture.CaptureWindow`) produces screenshots of sufficient quality and speed for post-operation feedback; no new capture technology is introduced.
- All mouse operations, including MOVE, require a bound window so coordinates are interpreted consistently as screenshot-relative coordinates in the selected window's pixel space.
- The 5 MiB screenshot size limit currently enforced for user-turn screenshots is a reasonable ceiling for post-operation screenshots as well.
- The proto `AgentOperationResultFrame` will be extended (not replaced) to carry an optional screenshot field, preserving backward compatibility with existing consumers that read only the status and message fields.
- The operation-bridge timeout (currently 5 seconds) is sufficient for the round-trip including screenshot capture; if capture adds significant latency, the timeout may need adjustment in a follow-up.

## References *(mandatory per Constitution §I — Citation Provenance)*

### Official Documentation

- No external references. This spec extends the existing mouse operation and screenshot capabilities already implemented in this repository (feature 013-agent-game-tools). No external APIs, standards, or third-party documentation are introduced.

### Repositories

- No external repositories referenced.

### Articles & RFCs

- No external articles or RFCs referenced.
