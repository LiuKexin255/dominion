<script lang="ts">
  // OperationConfirmDrawer — session-top debug drawer that asks the user to
  // approve each held operation result before the desktop returns it to the
  // agent (specs/023-saolei-mcp-refine/contracts/debug-drawer-contract.md §3.2,
  // research.md D11).
  //
  // Pure operation-channel surface: it associates with a held operation via
  // the bridge-minted operation id (`entry.toolId`), NOT via any conversation
  // `tool_call.id` (decoupled per research.md D10). The conversation renderer
  // (`ChatView`) is uninvolved. Rows stack vertically in arrival order; the
  // drawer is hidden when `heldOperations` is empty.

  import type { HeldOperation } from '../api'

  let {
    heldOperations = [],
    onConfirm = (_toolId: string) => {},
  }: {
    heldOperations?: HeldOperation[]
    onConfirm?: (toolId: string) => void
  } = $props()
</script>

{#if heldOperations.length > 0}
  <div class="op-confirm-drawer" data-testid="op-confirm-drawer">
    {#each heldOperations as entry (entry.toolId)}
      <div class="op-confirm-row" data-testid="op-confirm-row">
        <span class="op-confirm-icon" aria-hidden="true">&#9208;</span>
        <span class="op-confirm-summary" data-testid="op-confirm-summary">{entry.summary}</span>
        <button
          class="btn btn-small confirm-btn"
          data-testid="confirm-tool-result"
          onclick={() => onConfirm(entry.toolId)}
        >Confirm</button>
      </div>
    {/each}
  </div>
{/if}

<style>
  /* Drawer panel pinned to the top of the session chat area, visually
     separable from the conversation transcript below. */
  .op-confirm-drawer {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 8px 10px;
    background: rgba(139, 233, 253, 0.08);
    border: 1px solid rgba(139, 233, 253, 0.3);
    border-radius: 6px;
  }

  .op-confirm-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 4px 6px;
    background: rgba(139, 233, 253, 0.06);
    border: 1px solid rgba(139, 233, 253, 0.18);
    border-radius: 4px;
    font-size: 12px;
    color: #e0e0e0;
  }

  .op-confirm-icon {
    color: #8be9fd;
    font-size: 13px;
    flex-shrink: 0;
  }

  .op-confirm-summary {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  }

  /* Confirm control — reuses the existing `.btn btn-small` baseline; the
     color scheme mirrors the prior bubble Confirm so users see a familiar
     accent on the new surface. */
  .confirm-btn {
    margin-left: auto;
    background: rgba(139, 233, 253, 0.12);
    border: 1px solid rgba(139, 233, 253, 0.4);
    color: #8be9fd;
    font-weight: 600;
    flex-shrink: 0;
  }

  .confirm-btn:hover {
    background: rgba(139, 233, 253, 0.2);
  }
</style>
