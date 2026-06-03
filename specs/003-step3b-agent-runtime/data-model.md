# Data Model: Step3.b Agent Runtime

**Branch**: `003-step3b-agent-runtime` | **Date**: 2026-06-03

## Entities

### 1. Runtime Agent

Per-session TypeScript runtime object created only after profile, SKILL, MCP, model, and credential validation succeeds.

| Field | Type | Constraints | Description |
|-------|------|-------------|-------------|
| session_id | `string` | Required, one runtime agent per session | Session owning the agent |
| agent_profile_name | `string` | Required, references enabled prompt profile | Profile used at creation |
| owner_index | `int32` | Required for proxy owner routing | Existing ownership metadata returned in `Agent` |
| owner | `string` | Required | Owner identifier for this service instance |
| deep_agent | `DeepAgent` | Constructed via `createDeepAgent` | Single primary DeepAgent instance |
| loaded_profile | `LoadedAgentProfile` | Immutable during invoke | Snapshot of creation-time profile |
| loaded_skills | `LoadedSkill[]` | Enabled only | Tool-independent SKILL contents from prompt service |
| supported_mcps | `string[]` | Must all exist in runtime registry | Runtime-owned MCP/tool selections |
| lifecycle_state | `AgentLifecycleState` | See state transitions | Current runtime state |
| active_invoke | `InvokeCycle?` | Max one | Current reasoning pass |
| pending_operation | `PendingOperation?` | Max one | Operation awaiting next screenshot |
| last_activity_time | `timestamp` | Updated on accepted input/output/delete-relevant activity | Idle cleanup basis |
| create_time | `timestamp` | Output only | Agent creation time |

**Validation Rules**:
- `session_id` must be non-empty.
- `agent_profile_name` must be non-empty and resolve to an enabled prompt profile.
- All profile `skill_names` must resolve to enabled prompt SKILLS.
- All profile `mcp_names` must be present in the TypeScript runtime MCP registry.
- Provider validation must complete before persisting or returning the created agent.

### 2. Loaded Agent Profile

Creation-time profile snapshot retrieved from prompt service.

| Field | Type | Constraints | Description |
|-------|------|-------------|-------------|
| agent_profile_name | `string` | Required | Profile business id |
| model | `string` | Required for runtime | Default DeepAgents model ref or `opencode-go/<model-id>` |
| system_prompt | `string` | May be empty only if default runtime prompt is allowed by tasks | Passed to `createDeepAgent` |
| skill_names | `string[]` | Every entry enabled and loaded | Tool-independent SKILL references |
| mcp_names | `string[]` | Every entry supported | Runtime MCP/tool bundle names |
| enabled | `bool` | Must be true | Disabled profile rejects creation |

### 3. Loaded Skill

Tool-independent prompt-service guidance loaded into the DeepAgent configuration.

| Field | Type | Constraints | Description |
|-------|------|-------------|-------------|
| skill_name | `string` | Required | Prompt service skill id |
| content | `string` | Non-empty for acceptance profiles | Gameplay guidance content |
| enabled | `bool` | Must be true | Disabled skill rejects creation |
| source_update_time | `timestamp` | Optional | Used for diagnostics only |

### 4. Provider Credential

Secret-backed model provider credential used by the selected model path.

| Field | Type | Constraints | Description |
|-------|------|-------------|-------------|
| provider | enum | `default`, `opencode-go` | Selected from profile model ref |
| model_id | `string` | Required | For OpenCode Go, suffix after `opencode-go/` |
| secret_logical_name | `string` | Required for OpenCode Go | Deployment secret logical file name |
| credential_status | enum | `validated`, `missing`, `empty`, `unreadable`, `invalid`, `unauthorized`, `unsupported_model`, `malformed_model_ref` | Creation gate result |

**Validation Rules**:
- `opencode-go/` without model id is malformed.
- Unsupported OpenCode Go model id rejects creation.
- Missing/empty/unreadable secret file rejects creation.
- Unauthorized/invalid API response during validation rejects creation.
- No fallback to another provider is allowed.

### 5. Invoke Cycle

One screenshot-driven DeepAgent reasoning pass.

| Field | Type | Constraints | Description |
|-------|------|-------------|-------------|
| invoke_id | `string` | Unique per cycle | Links streamed frames |
| input_screenshot_id | `string` | Required | Screenshot that triggered the invoke |
| input_sequence | `int64` | Strictly increasing | Protocol order from desktop |
| start_time | `timestamp` | Required | Timeout basis |
| deadline | `timestamp` | Default start + 10m | Invoke cancellation time |
| output_sequence | `int64` | Monotonic | Agent frame sequence |
| status | enum | `running`, `completed`, `timed_out`, `failed`, `cancelled` | Invoke state |
| emitted_operation_id | `string?` | At most one | Set when operation tool succeeds |

### 6. Pending Operation

Desktop operation emitted by the agent and awaiting screenshot-only continuation.

| Field | Type | Constraints | Description |
|-------|------|-------------|-------------|
| operation_id | `string` | Required | Matches `AgentOperationFrame.operation_id` |
| screenshot_id | `string` | Required | Screenshot the operation is based on |
| operation_type | enum | mouse or keyboard | Supported desktop action |
| x_px/y_px | `int32?` | Mouse only, within screenshot bounds | Screenshot-relative coordinates |
| key_codes | `string?` | Keyboard only, non-empty | Keyboard action |
| expected_next_sequence_min | `int64` | Strict monotonic continuation | Reject stale screenshots |
| state | enum | `waiting_for_screenshot`, `continued`, `rejected`, `expired` | Continuation state |

### 7. Play Conversation Item

Desktop UI item derived from streamed frames and local execution state.

| Field | Type | Constraints | Description |
|-------|------|-------------|-------------|
| item_id | `string` | Required | UI identity |
| role | enum | `desktop`, `agent`, `local` | Message owner |
| kind | enum | screenshot, thinking, text, tool_progress, operation, warning, execution_status | Visual classification |
| collapsed | `bool` | Screenshot defaults true | UI display state |
| frame_id | `string?` | Present for agent/desktop frame-backed items | Source frame |
| content | object/string | Kind-specific | Rendered data |

## State Transitions

```text
Runtime Agent
  creating
    ├─ validation failure → not_created(error)
    └─ validation success → idle

  idle
    ├─ accepted screenshot → invoking
    ├─ DeleteAgent → deleted
    └─ idle > 30m and no pending operation → deleted

  invoking
    ├─ DeepAgent text/thinking/tool event → invoking (stream frame)
    ├─ operation tool emits valid operation → waiting_for_screenshot
    ├─ invoke completes without operation → idle
    ├─ invoke deadline > 10m → timed_out → idle
    └─ DeleteAgent / stream cancellation → deleted or idle according to RPC context

  waiting_for_screenshot
    ├─ next valid screenshot → invoking
    ├─ stale/out-of-order screenshot → waiting_for_screenshot + warn frame
    ├─ DeleteAgent → deleted
    └─ idle cleanup skipped while pending operation remains
```

## Relationships

```text
PromptService.AgentProfile
  ├── skill_names ───────────────┐
  ├── mcp_names ───────┐         │
  └── model ───────┐   │         │
                   ▼   ▼         ▼
ProviderCredential  MCPRegistry  PromptService.Skill
        │              │         │
        └──────────────┴─────────┘
                       ▼
                 Runtime Agent
                       │
                       ▼
                 Invoke Cycle
                       │
                       ▼
              AgentFrame stream / Pending Operation
                       │
                       ▼
             Desktop Play Conversation Item
```

## Validation Matrix

| Scenario | Expected Result |
|----------|-----------------|
| Missing profile | `CreateAgent` fails with configuration error |
| Disabled profile | `CreateAgent` fails with configuration error |
| Missing/disabled SKILL | `CreateAgent` fails and agent is not usable |
| Unsupported MCP name | `CreateAgent` fails naming unsupported MCP |
| Malformed `opencode-go/` model ref | `CreateAgent` fails with model configuration error |
| Missing/empty/unreadable OpenCode Go secret | `CreateAgent` fails with provider credential error |
| Unauthorized OpenCode Go key | `CreateAgent` fails with provider credential error |
| Valid screenshot while idle | Starts invoke and streams progressive frames |
| Agent emits out-of-bounds mouse coordinates | Emit warning or invalid operation status; desktop must not execute |
| Second screenshot before operation completion but stale sequence | Warn and do not advance pending operation |
| Invoke exceeds 10 minutes | Cancel invoke and stream timeout status/warning |
| Idle >30 minutes with no invoke/pending operation | Agent auto-deleted |
| Delete missing agent | Success, no error noise |
