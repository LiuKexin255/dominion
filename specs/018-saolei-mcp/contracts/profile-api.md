# Contract: Agent Profile MCP & Skill Selection

**Feature**: 018-saolei-mcp | Spec: US3, FR-031..035

This contract defines how an operator selects MCPs and skills on an agent profile through the desktop UI, and how those selections travel from the UI to the persisted profile and onward to the agent adapter. The proto `AgentProfile` already carries `skill_names`/`mcp_names`/`tool_names` and a `Skill` resource exists — this feature **wires the dormant fields end-to-end** (FR-033, FR-034).

## Data already present (reused — no proto schema change)

From `projects/game/game.proto`:

```proto
message AgentProfile {
  ...
  repeated string skill_names = 5;
  repeated string mcp_names   = 6;
  repeated string tool_names  = 10;
  ...
}
message Skill { ... }   // name, skill_name, content, enabled, ...
service PromptService {
  rpc CreateAgentProfile(CreateAgentProfileRequest) returns (AgentProfile);
  rpc UpdateAgentProfile(UpdateAgentProfileRequest) returns (AgentProfile);  // FieldMask
  rpc GetSkill(GetSkillRequest) returns (Skill);
  rpc ListSkills(...) returns (ListSkillsResponse);
  ...
}
```

## UI → Wails view-model (修改 — close the dormant gap)

`projects/game/desktop/view_model.go` — `CreateAgentProfileView` currently **omits** `SkillNames`/`McpNames` (the dormant gap FR-033 closes). The contract:

| View-model field | Type | Notes |
|---|---|---|
| `CreateAgentProfileView.SkillNames` | `[]string` | **新增** (was omitted); JSON `skillNames` |
| `CreateAgentProfileView.McpNames` | `[]string` | **新增** (was omitted); JSON `mcpNames` |

(`AgentProfileView` already carries `SkillNames`/`McpNames` — only the create path needs the fix.)

## Frontend (Svelte) — `ProfileManagement.svelte` (修改)

Add MCP + skill selection to **both** the create and edit forms, mirroring the existing `toolNames` chip pattern (`VALID_TOOL_VALUES` + `toggleTool`):

- A `VALID_MCP_VALUES` set (v1: `{"saolei"}`) drives the MCP chips.
- A `VALID_SKILL_VALUES` set (v1: `{"saolei"}`) drives the skill chips.
- Create form: `formMcpNames`, `formSkillNames` state; submitted in `CreateAgentProfileRequest`.
- Edit form: `editMcpNames`, `editSkillNames` state; the update `FieldMask` paths MUST include `"mcp_names"` and `"skill_names"` (FR-032).

**Unknown values**: names not in the valid sets are filtered out on load (like `filterValidTools`) and surfaced as a warning (FR-035).

## Frontend API — `api.ts` (修改)

`CreateAgentProfileRequest` already has optional `skillNames`/`mcpNames` (api.ts lines ~249-257). The `createAgentProfile` wrapper must actually send them. `updateAgentProfile` already takes a full `AgentProfile` (carrying the names) + `updateMaskPaths` — ensure the mask includes the two new paths.

## End-to-end flow

```
Operator (ProfileManagement.svelte)
  │  toggles saolei mcp + saolei skill chips
  ▼
CreateAgentProfileView / UpdateAgentProfileRequest  (view_model.go)
  │  skillNames, mcpNames included (FR-033)
  ▼
PromptService.Create/UpdateAgentProfile  (game.proto) → persisted AgentProfile
  │  skill_names, mcp_names stored
  ▼
prompt-client.ts getProfile()  (修改: also GetSkill for each skill_name → skillContents)
  │  ProfileData { toolNames, skillNames, mcpNames, skillContents }
  ▼
AdapterFactory → AgentAdapterImpl  (llm.ts)
  │  buildTools(toolNames, mcpNames, bridge) → resolves "saolei" mcp → 5 saolei tools
  │  effectiveSystemPrompt = systemPrompt + SEPARATOR + skillContents  (D-2)
  ▼
createAgent({ model, systemPrompt: effective, tools: [...existing, ...saolei], ... })
```

## Selection semantics

- A profile may declare zero or more MCPs and zero or more skills (FR-031).
- Declaring `mcp_names: ["saolei"]` exposes the five saolei tools to that profile's agent (FR-034).
- Declaring `skill_names: ["saolei"]` injects the saolei skill content into the system prompt at agent creation (FR-026, D-2).
- Skills are resolved at agent creation (static for the session — FR-027; dynamic injection out of scope).
- Unknown names are ignored + warned (FR-035).
- Mid-session profile edits trigger `RefreshAgent` → adapter rebuild → new `SaoleiMcp` instance at `uninitialized` (FR-025c); an in-flight turn is rejected until refresh completes (existing `RefreshAgent` contract).
