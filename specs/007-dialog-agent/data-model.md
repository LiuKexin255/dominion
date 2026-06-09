# Data Model: Dialog Agent with Chat Interface

## Agent Profile

Existing prompt-service resource used as the blueprint for an agent instance.

| Field | Type | Rules |
|---|---|---|
| `agentProfileName` | string | Unique business identifier; required for agent creation. |
| `model` | string | Provider/model selector copied into the agent instance at creation time. |
| `systemPrompt` | string | Descriptive prompt copied into the agent instance at creation time. |
| `enabled` | boolean | Disabled profiles cannot be used for new agent creation. |
| `skillNames`, `mcpNames` | string[] | Existing fields may remain stored, but this feature does not enable tools/MCP/skills at runtime. |

**Lifecycle rule**: Profiles are loosely coupled from active agent instances after creation. Deleting or editing a profile affects future agent creation only.

## Agent Instance

Stateful runtime object bound to one session and one agent service instance.

| Field | Type | Rules |
|---|---|---|
| `sessionId` | string | Unique key for active instance. |
| `profileName` | string | Profile name used at creation. |
| `copiedModel` | string | Copied from profile at creation time. |
| `copiedSystemPrompt` | string | Copied from profile at creation time. |
| `status` | `idle`, `processing`, `waiting`, or `failed` | Displayed in sidebar and used for cleanup. |
| `history` | ConversationMessage[] | Full current-session conversation context. |
| `queue` | UserMessage[] | Messages received while `status=processing`, processed FIFO. |
| `lastActivityAt` | timestamp | Updated when a message is received or processing completes. |
| `createdAt` | timestamp | Output-only metadata. |
| `cleanupDeadline` | timestamp | `lastActivityAt + 15 minutes` when not processing. |

**State transitions**:

```text
created -> idle
idle + user message -> processing
processing + model success -> idle (then process next queued message if present)
processing + model/provider error -> failed or idle with error frame
idle with no messages for >15 minutes -> cleaned up
processing -> preserved by cleanup check
```

## Conversation Message

Single chronological chat entry displayed in the desktop dialog area and supplied as context for future model calls.

| Field | Type | Rules |
|---|---|---|
| `messageId` | string | Unique within session/agent instance. |
| `sessionId` | string | Required. |
| `role` | `user`, `thinking`, `assistant`, or `error` | Determines rendering and model context usage. |
| `content` | string | Text content. Provider secrets must never appear. |
| `createdAt` | timestamp | Used for chronological ordering. |
| `turnId` | string | Groups user input, thinking output, and final response for one turn. |

**Ordering rule**: User messages, thinking output, and final response are appended in chronological order. Messages submitted while processing are queued and processed after the current turn.

## Provider Credential

Runtime-only secret material used by the LLM adapter.

| Field | Type | Rules |
|---|---|---|
| `secretPath` | string | Deployment-provisioned mounted file path. |
| `secretValue` | string | Empty string when file is missing; never logged or displayed. |
| `providerName` | string | This version uses the configured single provider. |

**Validation rule**: Missing secret file is valid and yields `secretValue=""`. Existing unreadable or malformed files produce descriptive errors without exposing contents.

## LLM Adapter

Internal module boundary that converts conversation context into provider/model calls.

| Method | Input | Output | Rules |
|---|---|---|---|
| `generateTurn` | copied prompt, history, user message, provider secret | thinking text + final text | Production adapter may call provider; test adapter must be deterministic and avoid real provider traffic. |

## Testplan Suite

One suite inside `projects/game/testplan/system_test.yaml`.

| Field | Type | Rules |
|---|---|---|
| `name` | string | Unique suite name. |
| `deploy` | label | Suite-specific deploy YAML with only needed services. |
| `endpoint.http.public` | URL | Gateway HTTP endpoint consumed through `testtool.MustEndpoint("http", "public")`. |
| `cases` | Bazel labels | Suite-specific `go_largetest` targets. |

**Deployment rule**: Prompt/profile suites must not deploy agent/proxy. Agent/dialog suites must use fake LLM wiring and must not send messages to a real provider.
