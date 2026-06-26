# Feature Specification: Desktop Agent Interaction Refinement

**Feature Branch**: `015-desktop-agent-refinement`

**Created**: 2026-06-26

**Status**: Draft

**Input**: User description: "可以实现 agent 连续操作，但还有些问题需求调整。新增一个需求以修改: 1. 将 mouse 工具进行拆分，显式分为 mouse_move 和 mouse_click，mouse_move 仅负责移动，mouse_click 仅负责操作，避免 agent 输出错误坐标导致误操作。2. agent 连续操作期间 desktop 对话框无法正常更新，必须等 agent 完全停止才会刷新（截图内容在 agent 输出完成后再消失）。3. 对话页面 tools 操作气泡展示 result 内容（默认折叠），历史消息展示包括工具操作和返回，且与正常对话展示一致。4. 截图不包括鼠标，desktop 改用 win32 api 在截图上绘制鼠标；增加截图按键快捷键方便测试；user 发送消息附加截图时可点击放大查看。"

**Relationship**: Refines and extends [feature 014 — Mouse Move Action & Post-Operation Screenshot Feedback](../014-mouse-move-screenshot/spec.md). Feature 014 established continuous agent mouse operations with post-action screenshots; this feature addresses practical issues discovered during real use.

## Clarifications

### Session 2026-06-26

- Q: Should `mouse_click` accept coordinates, or should coordinates belong only to `mouse_move`? → A: `mouse_click` clicks at the current cursor position; coordinates belong only to `mouse_move`.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Real-Time Desktop Dialog Update During Agent Operations (Priority: P1)

When the agent performs a continuous sequence of operations (multiple tool calls, reasoning, and responses in a single run), the desktop conversation dialog renders every piece of content — streaming text, tool results, screenshots — as it is produced, not in a single batch after the entire run completes. The user can watch the agent work in real time and intervene if something goes wrong.

**Why this priority**: In practice, the dialog currently freezes during continuous operations and only refreshes after the agent fully stops. The user cannot monitor what the agent is doing, cannot tell if it is heading in the wrong direction, and cannot see screenshots being consumed until it is too late. This blocks effective human oversight of autonomous operations and is the single most impactful issue to fix.

**Independent Test**: Start a continuous agent operation involving multiple tool calls in one run. Observe the dialog: streaming text, tool results, and screenshot consumption should all appear in real time as they occur, not deferred to the end of the run.

**Acceptance Scenarios**:

1. **Given** the agent is mid-run in a continuous operation producing streaming text, **When** each text chunk is generated, **Then** the desktop dialog renders it within perceptual real-time (under 1 second), so the user sees incremental progress before the run completes.
2. **Given** a screenshot-bearing user message is sent to the agent during a continuous run, **When** the agent has consumed that screenshot, **Then** the screenshot is removed from the pending display immediately upon consumption, not held until the full run completes.
3. **Given** multiple tool results are produced during a continuous run, **When** each tool result arrives, **Then** each result is reflected in the dialog as it arrives, in chronological order, so the user can follow the agent's tool usage step by step.
4. **Given** the agent is performing a long continuous operation, **When** the user observes the dialog, **Then** they see incremental, live updates (text streaming, tool results appearing, screenshots updating) rather than a frozen view that only refreshes at the end.

---

### User Story 2 - Mouse Tool Split: mouse_move and mouse_click (Priority: P2)

The agent's mouse interaction is restructured from a single combined tool (with action-based selection between MOVE and click variants) into two distinct, focused tools: `mouse_move` (cursor repositioning only, no button events) and `mouse_click` (button operations only at the current cursor position, with no coordinate parameters). Each tool has a smaller, clearer parameter space, reducing the likelihood that the agent outputs a wrong coordinate or confuses action types — the practical cause of misoperations identified during use.

**Why this priority**: The combined tool requires the agent to simultaneously choose the correct action type and the correct coordinates. In practice, this led to misoperations where the agent output wrong coordinates. Splitting into two focused tools narrows each tool's decision space, making errors structurally less likely. This is secondary to US1 because real-time monitoring (US1) is the prerequisite for even detecting misoperations.

**Independent Test**: Instruct the agent to reposition the cursor using `mouse_move`, then click at the current cursor position using `mouse_click`. Each tool executes independently with a focused parameter set, and both return post-action screenshots (inheriting feature 014).

**Acceptance Scenarios**:

1. **Given** the agent needs to reposition the cursor without clicking, **When** it invokes `mouse_move` with target coordinates, **Then** the cursor moves to the specified position, no button events are dispatched, and a post-action screenshot is returned if a window is bound.
2. **Given** the agent needs to perform a click after positioning the cursor, **When** it invokes `mouse_click` with a click type, **Then** the appropriate click action occurs at the current cursor position, and a post-action screenshot is returned if a window is bound.
3. **Given** no window is bound, **When** either `mouse_move` or `mouse_click` is invoked, **Then** the operation fails with the same no-window-bound behavior established in feature 014.
4. **Given** a historical conversation that used the previous combined mouse tool, **When** it is displayed in the history view, **Then** the old tool operations render correctly without errors.

---

### User Story 3 - Screenshot Cursor Fidelity, Keyboard Shortcut, and Click-to-Zoom (Priority: P3)

Screenshots captured by the desktop faithfully include the real operating-system cursor at its actual on-screen position (not a self-drawn approximation), so the agent and user can see exactly where the cursor is. A keyboard shortcut enables screenshot capture without mouse interaction, making it possible to test cursor-in-screenshot scenarios — triggering capture via mouse-click would displace the cursor. User-attached screenshots in outgoing messages support click-to-zoom for detailed inspection, since current preview thumbnails are too small to read.

**Why this priority**: Without the real cursor in screenshots, the agent's post-action visual feedback (feature 014) cannot show where the cursor actually landed — undermining the entire perception-action loop. The keyboard shortcut and click-to-zoom are testing and usability enablers that make the cursor feature verifiable and practical. This is third because US1 (monitoring) and US2 (safety) must work first.

**Independent Test**: Position the cursor at a known location, capture a screenshot via keyboard shortcut, and verify the cursor appears in the screenshot at the correct position. Attach a screenshot to a message, click the preview, and verify it opens at full size.

**Acceptance Scenarios**:

1. **Given** a screenshot is captured while the cursor is visible on screen, **When** the screenshot is delivered, **Then** it includes the actual operating-system-rendered cursor at its real position, not a self-drawn approximation overlaid by the application.
2. **Given** the user wants to capture a screenshot showing the cursor at a specific position, **When** they position the cursor and press the designated keyboard shortcut, **Then** a screenshot is captured at the current cursor position without any cursor displacement caused by the capture trigger.
3. **Given** the user has attached a screenshot to an outgoing message, **When** they click the preview thumbnail, **Then** the screenshot opens at a larger size showing full detail, and can be dismissed to return to the composition view.

---

### User Story 4 - Tool Result Bubble Display and Historical Conversation Consistency (Priority: P4)

Tool operation bubbles in the conversation display their result content — collapsed by default with an expand option — so the user can inspect what each tool returned without cluttering the view. Historical conversations include all tool operations and their results, rendered identically to how they appeared during the live conversation, eliminating any discrepancy between live and historical rendering.

**Why this priority**: Seeing tool results helps the user understand and debug agent behavior. Historical consistency ensures past conversations are as informative as live ones. This is the lowest priority because it is a visibility improvement that builds on the real-time updates (US1) and tool restructuring (US2).

**Independent Test**: Run a conversation with tool operations. Verify tool bubbles show collapsed results that expand on click. Reload the same conversation from history and verify tool operations, results, and layout are identical to the live view.

**Acceptance Scenarios**:

1. **Given** a tool operation completes during a conversation, **When** the tool bubble is displayed, **Then** it shows the tool result content collapsed by default, with a clear affordance indicating the result can be expanded.
2. **Given** a tool bubble with collapsed results is displayed, **When** the user activates the expand control, **Then** the full result content is revealed.
3. **Given** a historical conversation contained tool operations, **When** it is loaded in the history view, **Then** all tool operations and their results are displayed — none are omitted.
4. **Given** a conversation was originally rendered live with tool bubbles and results, **When** the same conversation is viewed from history, **Then** the rendering is identical in layout, tool bubble display, and result visibility.

---

### Edge Cases

- What happens when a tool result is very large (e.g., a high-resolution screenshot or a long text block)? The collapsed bubble should show a truncated summary; expansion reveals the full content, and very large content should remain scrollable within the bubble.
- What happens when the keyboard screenshot shortcut conflicts with an existing application or operating-system shortcut? The shortcut should be chosen to avoid known conflicts; if a conflict is discovered, it should be reassignable.
- What happens when the cursor is at a screen edge or partially off-screen when a screenshot is taken? The cursor should still be rendered at its actual (possibly clipped) position, matching what the user sees.
- What happens to the old combined mouse tool in existing agent prompts or saved conversations? Historical conversations must still render old-format tool calls correctly; the tool split is a forward change that does not break historical display.
- What happens if the real-time update encounters a rendering error mid-stream? The dialog should recover gracefully, showing the content that was successfully received and indicating any gap.

## Requirements *(mandatory)*

### Functional Requirements

**Mouse Tool Split**

- **FR-001**: The agent's mouse interaction MUST be exposed as two distinct tools — `mouse_move` (cursor repositioning only) and `mouse_click` (button operations only at the current cursor position) — replacing the previous single combined mouse tool.
- **FR-002**: The `mouse_move` tool MUST accept target coordinates and reposition the cursor to that position without dispatching any button events.
- **FR-003**: The `mouse_click` tool MUST accept a click type and dispatch the appropriate button events at the current cursor position; it MUST NOT accept target coordinates.
- **FR-004**: Both mouse tools MUST require a bound window and MUST fail with the same no-window-bound behavior established in feature 014 when no window is selected.
- **FR-005**: Both mouse tools MUST return post-action screenshots (inheriting feature 014 behavior) when a window is bound, including the real operating-system cursor at its current position when visible.
- **FR-006**: The previous combined mouse tool (with action-based MOVE/click selection) MUST be removed and replaced by the two focused tools; historical conversations that recorded the old format MUST still render correctly.

**Real-Time Dialog Update**

- **FR-007**: The desktop conversation dialog MUST render streaming text, tool results, and content changes incrementally as they are produced during agent continuous operations — not deferred until the agent run completes.
- **FR-008**: Content that is consumed or superseded during agent operations (e.g., a screenshot that has been processed by the agent) MUST be updated in the dialog immediately upon consumption, not held in a pending state until agent completion.

**Screenshot Cursor, Shortcut, and Zoom**

- **FR-009**: Screenshots captured by the desktop MUST include the actual operating-system-rendered cursor at its real on-screen position, using the operating system's native cursor rendering rather than a self-drawn overlay.
- **FR-010**: The desktop MUST provide a keyboard shortcut to trigger screenshot capture without requiring mouse interaction, so screenshots can be taken at the current cursor position without displacement.
- **FR-011**: Screenshots attached by the user to outgoing messages MUST support click-to-open, displaying the screenshot at a larger or full-size view for detailed inspection, with a way to dismiss and return to the composition view.

**Tool Result Bubble and History**

- **FR-012**: Tool operation bubbles in the conversation MUST display the tool's result content collapsed by default, with a clear expand affordance to reveal the full result.
- **FR-013**: Historical conversation display MUST include all tool operations and their results — none may be omitted.
- **FR-014**: Historical conversation rendering MUST be identical to the live conversation rendering in layout, tool bubble display, and result visibility — no rendering discrepancies between live and historical views.

### Key Entities *(include if feature involves data)*

- **Agent Mouse Tools (restructured)**: The set of tools the agent uses for mouse interaction, restructured from one combined tool into two focused tools (`mouse_move` and `mouse_click`), where coordinates belong only to movement and click operations happen at the current cursor position.
- **Conversation View (real-time)**: The desktop's real-time dialog display, which must render streaming content, tool results, and content lifecycle changes (e.g., screenshot consumption) incrementally as they occur during continuous agent operations.
- **Tool Operation Bubble (extended)**: The UI element representing a tool invocation and its result, extended to display result content collapsed-by-default with expand capability, and to render identically in live and historical views.
- **Screenshot (cursor-faithful)**: The image captured from the desktop window, which must faithfully represent the actual screen state including the real operating-system cursor position, be triggerable via keyboard shortcut, and be inspectable at full size when user-attached.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: During agent continuous operations, the desktop dialog reflects each event (text chunk, tool result, screenshot consumption) within 1 second of occurrence, so users can monitor agent actions in real time in 100% of test cases.
- **SC-002**: After splitting the mouse tool, the agent produces 0 cross-action errors (clicking when it meant to move, or moving when it meant to click) in test scenarios, because each tool handles only one type of action.
- **SC-003**: Screenshots include the real operating-system cursor at its actual position in 100% of captures where the cursor is on-screen, so the agent and user can reliably determine cursor location from screenshots.
- **SC-004**: Users can inspect full-size detail of attached screenshots by clicking preview thumbnails, with the expanded view showing the screenshot at a resolution sufficient to read text and identify UI elements.
- **SC-005**: Historical conversations render tool operations, results, and layout identically to their original live rendering in 100% of test cases, with zero rendering discrepancies.

## Assumptions

- Feature 014 (mouse MOVE action + post-operation screenshot feedback) is implemented and serves as the baseline; this feature refines and extends it. See [feature 014 spec](../014-mouse-move-screenshot/spec.md).
- The `mouse_click` tool retains all click-type variants (left click, right click, double-click, etc.) from the existing combined mouse tool, but it does not accept coordinates; positioning is always performed through `mouse_move` before clicking.
- The user explicitly specified the win32 API for cursor rendering as an implementation constraint; the specific API calls will be researched and cited in `plan.md` per Constitution §III (External Dependency Research).
- The keyboard shortcut for screenshot capture will be chosen to avoid conflicts with existing operating-system and application-level shortcuts; the specific key combination is a plan-phase decision.
- The real-time dialog update fix targets the desktop conversation view's rendering and update pipeline; the specific async mechanism (event loop, reactive streams, state synchronization) is a plan-phase concern, not specified in this spec.
- Historical conversations that used the old combined mouse tool format must still render correctly after the split; the tool name/action format change is forward-only and does not require migrating stored conversation data.
- The post-action red-ring overlay from feature 014 is replaced by real operating-system cursor rendering; screenshots should no longer rely on a self-drawn overlay.

## References *(mandatory per Constitution §I — Citation Provenance)*

### Official Documentation

- No external references for the specification phase. The win32 cursor rendering API and keyboard shortcut implementation details will be researched and cited in `plan.md` per Constitution §III (External Dependency Research).

### Repositories

- No external repositories referenced. This spec extends the existing mouse operation and desktop capabilities implemented in this repository (features 013 and 014).

### Articles & RFCs

- No external articles or RFCs referenced.
