# Contract: Agent Profile MCP & Skill Selection

**Feature**: 018-saolei-mcp | Spec: US3, FR-031..035

This contract defines how an operator selects MCPs and skills on an agent profile through the desktop UI, and how those selections travel from the UI to the persisted profile and onward to the agent adapter. The proto `AgentProfile` already carries `skill_names`/`mcp_names`/`tool_names` and a `Skill` resource exists — this feature **wires the dormant fields end-to-end** (FR-033, FR-034).

> **Proto baseline note**: the request-message shapes below reflect the repo's AIP-131/132/133/134/156 compliance refactor ([AIP-133](https://google.aip.dev/133), [AIP-134](https://google.aip.dev/134), [AIP-156](https://google.aip.dev/156)) — `CreateAgentProfileRequest`/`UpdateAgentProfileRequest`/`CreateSkillRequest` are nested-resource shaped, and `AgentProfile`/`Skill` resource patterns live under the `prompts/` singleton namespace. 018 adds NO proto schema change for the profile data model; it wires the existing fields through the UI and the (now nested) request paths.

## Data already present (reused — no proto schema change)

From `projects/game/game.proto`:

```proto
message AgentProfile {                       // resource pattern: prompts/agentProfiles/{id}
  ...
  repeated string skill_names = 5;
  repeated string mcp_names   = 6;
  repeated string tool_names  = 10;
  ...
}
message Skill {                              // resource pattern: prompts/skills/{id}
  string name = 1;                           // (skill_name field removed — reserved 2)
  string content = 3;
  bool enabled = 4;
  ...
}
service PromptService {
  // AIP-133: identity + body on the nested resource, not flat request fields.
  rpc CreateAgentProfile(CreateAgentProfileRequest) returns (AgentProfile);
  // AIP-134: identity on agent_profile.name; FieldMask on update_mask.
  rpc UpdateAgentProfile(UpdateAgentProfileRequest) returns (AgentProfile);
  rpc GetSkill(GetSkillRequest) returns (Skill);
  rpc ListSkills(...) returns (ListSkillsResponse);
  ...
}

// AIP-133 nested create — skill_names/mcp_names live ON the nested AgentProfile.
message CreateAgentProfileRequest {
  string parent = 1;                         // == "prompts" (gameconst.PromptsParent)
  string agent_profile_id = 2;
  AgentProfile agent_profile = 3;
}
// AIP-134 nested update — no standalone name; identity on agent_profile.name.
message UpdateAgentProfileRequest {
  AgentProfile agent_profile = 2;
  google.protobuf.FieldMask update_mask = 3;
}
```

## UI → Wails view-model (close the dormant gap)

`projects/game/desktop/view_model.go` — `CreateAgentProfileView` currently **omits** `SkillNames`/`McpNames` (the dormant gap FR-033 closes). The contract:

| View-model field | Type | Notes |
|---|---|---|
| `CreateAgentProfileView.SkillNames` | `[]string` | was omitted (now added); JSON `skillNames` |
| `CreateAgentProfileView.McpNames` | `[]string` | was omitted (now added); JSON `mcpNames` |

(`AgentProfileView` already carries `SkillNames`/`McpNames` — only the create path needs the fix.)

## Frontend (Svelte) — `ProfileManagement.svelte`

Add MCP + skill selection to **both** the create and edit forms, mirroring the existing `toolNames` chip pattern (`VALID_TOOL_VALUES` + `toggleTool`):

- A `VALID_MCP_VALUES` set (v1: `{"saolei"}`) drives the MCP chips.
- A `VALID_SKILL_VALUES` set (v1: `{"saolei"}`) drives the skill chips.
- Create form: `formMcpNames`, `formSkillNames` state; submitted in `CreateAgentProfileRequest`.
- Edit form: `editMcpNames`, `editSkillNames` state; the update `FieldMask` paths MUST include `"mcp_names"` and `"skill_names"` (FR-032).

**Unknown values**: names not in the valid sets are filtered out on load (like `filterValidTools`) and surfaced as a warning (FR-035).

## Frontend API — `api.ts`

`AgentProfile` (api.ts) already carries `skillNames`/`mcpNames` — the read path is sound. The **create** path needs work: api.ts's hand-maintained `CreateAgentProfileRequest` interface is the *pre-refactor* flat shape (`agentProfileName`/`model`/`skillNames`/...) and no longer matches the AIP-133 nested proto (`{parent, agent_profile_id, agent_profile}`). Because the Wails Go binding (`app.go`) mediates between the TS view and the generated proto, the create-path wiring task MUST reconcile this: either align the TS interface to the nested shape, or keep the flat view-shape and let `app.go` assemble the nested `AgentProfile` (the latter is the smaller change — see the `app.go` mapping below). The `updateAgentProfile` wrapper already takes a full `AgentProfile` + `updateMaskPaths` — ensure the mask includes the two new paths.

## Go mapping — `app.go` `CreateAgentProfile` (closes FR-033 create gap)

`app.go`'s `CreateAgentProfile` already maps the Wails `CreateAgentProfileView` → the nested AIP-133 `game.CreateAgentProfileRequest{Parent, AgentProfileId, AgentProfile}`. Today that nested `AgentProfile` sets only `Model/SystemPrompt/Enabled/ToolNames` — **`SkillNames`/`McpNames` are dropped on the create path** (the FR-033 gap). Once `CreateAgentProfileView` carries the two new fields (above), this mapping MUST set `AgentProfile.SkillNames`/`AgentProfile.McpNames` so the selections reach the persisted profile. The **update** path (`app.go` `UpdateAgentProfile`) already sets `SkillNames`/`McpNames` on the proto `AgentProfile` — no change needed there.

## End-to-end flow

```
Operator (ProfileManagement.svelte)
  │  toggles saolei mcp + saolei skill chips
  ▼
CreateAgentProfileView  (view_model.go — += SkillNames/McpNames)
  │  skillNames, mcpNames now carried (FR-033)
  ▼
app.go CreateAgentProfile  (map view → nested AgentProfile{SkillNames,McpNames,...})
  │  AIP-133 CreateAgentProfileRequest{Parent, AgentProfileId, AgentProfile}
  ▼
PromptService.CreateAgentProfile  (game.proto) → persisted AgentProfile
  │  skill_names, mcp_names stored
  ▼
prompt-client.ts getProfile()  (also GetSkill for each skill_name → skillContents)
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
