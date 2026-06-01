<script lang="ts">
  import type { LogEntry } from '../logger'

  let {
    logs,
    onclear
  }: {
    logs: LogEntry[]
    onclear?: () => void
  } = $props()

  let logContainer: HTMLDivElement | undefined = $state()

  $effect(() => {
    if (logs.length && logContainer) {
      logContainer.scrollTop = logContainer.scrollHeight
    }
  })

  function formatTime(t: string): string {
    return new Date(t).toTimeString().slice(0, 8)
  }
</script>

<div class="log-panel">
  <div class="log-panel-header">
    <span>Logs ({logs.length})</span>
    <button class="btn btn-small" onclick={() => onclear?.()}>Clear Logs</button>
  </div>
  <div class="log-panel-container" bind:this={logContainer}>
    {#if logs.length === 0}
      <div class="log-panel-empty">No logs yet.</div>
    {/if}
    {#each logs as entry}
      <div class="log-line log-{entry.level.toLowerCase()}">
        <span class="log-time">{formatTime(entry.time)}</span>
        <span class="log-level">{entry.level.toUpperCase()}</span>
        <span class="log-source">[{entry.source}]</span>
        <span class="log-msg">{entry.message}</span>
        {#if entry.fields && Object.keys(entry.fields).length > 0}
          <span class="log-fields">{JSON.stringify(entry.fields, null, 2)}</span>
        {/if}
      </div>
    {/each}
  </div>
</div>

<style>
  .log-panel {
    display: flex;
    flex-direction: column;
    background: #16213e;
    border-radius: 6px;
    border: 1px solid #0f3460;
    overflow: hidden;
  }

  .log-panel-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 6px 10px;
    border-bottom: 1px solid #0f3460;
    font-size: 12px;
    color: #a0a0b0;
  }

  .log-panel-container {
    max-height: 300px;
    overflow-y: auto;
    overflow-x: auto;
    padding: 4px;
  }

  .log-panel-empty {
    padding: 20px;
    text-align: center;
    color: #606080;
    font-size: 12px;
  }

  .log-line {
    display: flex;
    gap: 8px;
    padding: 3px 6px;
    padding-left: 10px;
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 11px;
    line-height: 1.4;
    word-break: break-word;
    border-bottom: 1px solid #1a1a3e;
    border-left: 3px solid #606080;
  }

  .log-line:hover {
    background: #1a1a3e;
  }

  .log-line.log-error {
    border-left-color: #ff6b6b;
    background: rgba(255, 107, 107, 0.08);
    color: #ff6b6b;
  }

  .log-line.log-info {
    border-left-color: #808090;
    color: #c0c0d0;
  }

  .log-time {
    color: #606080;
    flex-shrink: 0;
  }

  .log-level {
    flex-shrink: 0;
    min-width: 40px;
    font-weight: 600;
  }

  .log-source {
    color: #4a9eff;
    flex-shrink: 0;
  }

  .log-msg {
  }

  .log-fields {
    color: #8be9fd;
    margin-left: 4px;
  }
</style>
