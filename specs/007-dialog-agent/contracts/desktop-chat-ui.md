# Contract: Desktop Chat UI

## Surface

After the user connects to an agent for a session, the desktop displays a chat interface with:

- main dialog area;
- agent information sidebar;
- message input control;
- visible chronological conversation thread.

## Dialog entries

The UI renders these entry kinds distinctly:

| Kind | Source | Required display behavior |
|---|---|---|
| User message | Desktop input | Shows the text the user sent. |
| Thinking | Agent `thinking` frame | Visually separate from final response. |
| Assistant response | Agent final `text` frame | Displayed after thinking for the same turn. |
| Warning/error | Agent `warn` frame or transport error | User-visible and must not expose provider credentials. |

## Sidebar metadata

The sidebar shows at least:

- profile name used by the active agent;
- instance status (`idle`, `processing`, `waiting`, `failed`, or equivalent user-facing text);
- connection state.

## Interaction rules

- Sending a message while the agent is processing keeps the UI responsive and indicates the message is queued or pending.
- Queued messages appear in send order.
- If the agent instance was cleaned up while the chat is open, the next send displays a clear recovery path such as reconnecting or creating a new instance.

## Accessibility/localization baseline

The plan does not introduce a full localization requirement. The UI must still use semantic controls for inputs/buttons and avoid relying solely on color to distinguish user, thinking, and assistant entries.
