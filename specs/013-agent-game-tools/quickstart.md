# Quickstart: Agent Game Tools and Image Turns

## Prerequisites

- Read the relevant repository style documents under `style/` before implementation work.
- Use Bazel entrypoints from `AGENTS.md` for Go, pnpm, Gazelle, build, and test commands.
- For large-test validation, use the repository `testplan` skill after reading `style/large_test.md`.

## Validation Scenario 1: Text-plus-image turn

1. Build or launch the game desktop and connect it to a deployed game SUT.
2. Create a session and select an agent profile.
3. Bind a visible game window.
4. Capture a screenshot; verify it appears as a pending collapsed attachment.
5. Enter text and send the turn.
6. Expected outcome: the conversation shows one user turn with the text and collapsed image, and the agent receives one multipart user-turn frame (`AgentUserTurnFrame`) containing both text and screenshot data, from which it constructs a single LangChain multimodal `HumanMessage` using text and image content blocks as described in the contract.

## Validation Scenario 2: Image-only turn and removal

1. Bind a visible game window.
2. Capture a screenshot.
3. Remove the pending attachment.
4. Send text only and verify no image appears.
5. Capture another screenshot and send without text.
6. Expected outcome: image-only send is accepted; removed images are not displayed or sent.

## Validation Scenario 3: Profile-scoped mouse tool

1. Create profile `tool-profile` with `tool_names = ["mouse"]`.
2. Create profile `chat-profile` with missing or empty `tool_names`.
3. Configure deterministic fake LLM responses so `tool-profile` requests a mouse operation and `chat-profile` does not.
4. Send the same text-plus-image turn with each profile.
5. Expected outcome: only `tool-profile` produces an operation request; `chat-profile` produces no operation frame.
6. Use `UpdateAgentProfile` with a FieldMask to add `"mouse"` to `chat-profile`'s `tool_names`, then call `RefreshAgent` to reload the profile for an active session.
7. Expected outcome: after refresh, `chat-profile` can produce mouse operation requests on subsequent turns.

## Validation Scenario 4: Operation result without auto-screenshot

1. Use a tool-enabled profile and deterministic fake LLM response requesting a valid mouse action.
2. Send a screenshot turn.
3. Let desktop auto-execute the first requested operation.
4. Expected outcome: desktop emits exactly one operation result frame with success or failure, the conversation displays action/coordinates/status, the agent receives the result, and no follow-up screenshot is sent automatically.

## Validation Scenario 5: Safety and edge cases

1. Attempt to send an image payload over 5 MiB.
2. Request an out-of-bounds mouse coordinate through the fake LLM.
3. Render an agent response containing lists, code spans, links, and raw HTML.
4. Expected outcome: oversized image is rejected before send; out-of-bounds operation returns a failed result without clicking; markdown renders allowed formatting while raw HTML is stripped.

## Suggested Verification Commands

Run targeted checks after implementation, adjusting package targets to match generated task boundaries:

```bash
bazel run //:go -- fmt projects/game
bazel run //:gazelle projects/game
bazel test //projects/game/...
bazel run @pnpm -- --dir /mnt/code/dominion/projects/game/desktop/frontend build
```

For end-to-end validation, execute the game testplan through the `testplan` skill rather than invoking deployment scripts manually.

## References

### Official Documentation

- [LangChain JavaScript v1 migration notes: multimodal messages](https://docs.langchain.com/oss/javascript/migrate/langchain-v1)
- [Marked safe output guidance](https://github.com/markedjs/marked/blob/v18.0.5/docs/INDEX.md)
- [DOMPurify security goals and allow-list guidance](https://github.com/cure53/DOMPurify/wiki/Security-Goals-&-Threat-Model)

### Repositories

- No repository references.

### Articles & RFCs

- No article or RFC references.
