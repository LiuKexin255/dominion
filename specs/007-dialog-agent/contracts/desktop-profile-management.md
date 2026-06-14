# Contract: Desktop Agent Profile Management

## Surface

The desktop provides a dedicated profile management page reachable from the
sessions page via a navigation button. The page displays a list of all agent
profiles and a creation form.

## Navigation

- Entry point: "Agent Profiles" button on the sessions page.
- Back navigation: returns to the sessions page.
- The profile management page is independent of any session or agent instance.

## Profile list

The list shows every agent profile returned by the prompt service, ordered by
the backend's default sort. Each list entry displays at minimum:

| Field | Source | Display |
|---|---|---|
| Profile name | `AgentProfileView.agentProfileName` | Primary label. |
| Display name | `AgentProfileView.name` | Secondary label (falls back to `agentProfileName` when empty). |
| Model | `AgentProfileView.model` | Metadata badge. |
| System prompt | `AgentProfileView.systemPrompt` | Truncated preview (first ~80 chars or first line). |
| Enabled | `AgentProfileView.enabled` | Visual indicator (badge or dimmed entry). |
| Created | `AgentProfileView.createTime` | Formatted timestamp. |

An empty state ("No profiles yet. Create one above.") appears when the list is
empty or fails to load.

## Create profile form

A form at the top of the page (or toggled by a "New Profile" button) collects:

| Field | Required | Validation |
|---|---|---|
| Agent profile name | Yes | Non-empty string; no whitespace-only; uniqueness enforced by backend (409 AlreadyExists). |
| Model | No | Model spec in `{provider}/{model}` format (e.g., `opencode-go/deepseek-v4-pro`). Defaults to empty string. |
| System prompt | No | Multi-line text area. Defaults to empty string. |
| Enabled | No | Checkbox, defaults to `true`. |

On submit:
- The form calls the Wails binding, which calls `POST /api/v1/prompts/agentProfiles`.
- On success, the new profile is prepended to the list and the form resets.
- On error (network, 409, 400), an inline error message is shown without clearing the form.

## Delete profile

Each list entry has a delete control:

1. First click shows an inline confirmation ("Delete this profile? [Confirm] [Cancel]").
2. Confirm calls the Wails binding, which calls `DELETE /api/v1/prompts/agentProfiles/{agent_profile_name}`.
3. On success, the entry is removed from the list.
4. On error, an inline error message appears on the entry.

Per spec US-3 acceptance scenario 4 and data-model lifecycle rule: deleting a
profile used by an active agent instance is allowed; existing instances continue
running with the prompt copied at creation time. The UI does **not** need to
check for active instances before deletion.

## Error handling

- All errors surface as inline messages within the profile management page.
- Errors must not expose provider credentials or internal stack traces.
- Network errors show a retry affordance on the profile list.

## Accessibility/localization baseline

Consistent with the chat UI contract: semantic controls for inputs/buttons, no
reliance on color alone for enabled/disabled state, and keyboard-navigable form
and list.
