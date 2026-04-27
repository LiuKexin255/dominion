<script lang="ts">
  import { onMount, onDestroy } from 'svelte';

  interface LogEntry {
    timestamp: string;
    level: 'INFO' | 'WARN' | 'ERROR';
    message: string;
  }

  const MAX_ENTRIES = 200;

  let logs: LogEntry[] = [];
  let container: HTMLDivElement;
  let autoScroll = true;

  function addEntry(level: string, message: string): void {
    const entry: LogEntry = {
      timestamp: new Date().toLocaleTimeString(),
      level: level === 'WARN' || level === 'ERROR' ? level : 'INFO',
      message,
    };
    logs = [...logs, entry];
    if (logs.length > MAX_ENTRIES) {
      logs = logs.slice(-MAX_ENTRIES);
    }
  }

  function onLogEvent(data: { level?: string; message?: string }): void {
    if (data && data.message) {
      addEntry(data.level ?? 'INFO', data.message);
    }
  }

  $: if (autoScroll && container) {
    container.scrollTop = container.scrollHeight;
  }

  onMount(() => {
    // Wails v2 injects EventsOn via the generated runtime module or window.runtime.
    const wails = window.runtime;
    if (wails?.EventsOn) {
      wails.EventsOn('log:entry', onLogEvent);
    }
  });

  onDestroy(() => {
    const wails = window.runtime;
    if (wails?.EventsOff) {
      wails.EventsOff('log:entry');
    }
  });
</script>

<div class="log-panel">
  <div class="panel-header">
    <h3>Logs</h3>
    <label class="autoscroll-toggle">
      <input type="checkbox" bind:checked={autoScroll} />
      Auto-scroll
    </label>
  </div>
  <div class="log-entries" bind:this={container}>
    {#each logs as entry}
      <div class="log-entry log-{entry.level.toLowerCase()}">
        <span class="log-time">{entry.timestamp}</span>
        <span class="log-level level-{entry.level.toLowerCase()}">{entry.level}</span>
        <span class="log-msg">{entry.message}</span>
      </div>
    {/each}
    {#if logs.length === 0}
      <div class="log-empty">No log entries yet.</div>
    {/if}
  </div>
</div>

<style>
  .log-panel {
    display: flex;
    flex-direction: column;
    height: 100%;
    background: #1e293b;
    border-radius: 6px;
    overflow: hidden;
  }

  .panel-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 12px;
    border-bottom: 1px solid #334155;
  }

  .panel-header h3 {
    margin: 0;
    font-size: 13px;
    font-weight: 600;
    color: #e2e8f0;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .autoscroll-toggle {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 11px;
    color: #94a3b8;
    cursor: pointer;
  }

  .autoscroll-toggle input {
    cursor: pointer;
  }

  .log-entries {
    flex: 1;
    overflow-y: auto;
    padding: 4px 0;
    font-family: 'Consolas', 'Monaco', 'Courier New', monospace;
    font-size: 12px;
    line-height: 1.6;
  }

  .log-entry {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    padding: 2px 12px;
    border-left: 3px solid transparent;
  }

  .log-entry:hover {
    background: rgba(255, 255, 255, 0.03);
  }

  .log-info {
    border-left-color: #3b82f6;
  }

  .log-warn {
    border-left-color: #f59e0b;
    background: rgba(245, 158, 11, 0.05);
  }

  .log-error {
    border-left-color: #ef4444;
    background: rgba(239, 68, 68, 0.05);
  }

  .log-time {
    flex-shrink: 0;
    color: #64748b;
    font-size: 11px;
  }

  .log-level {
    flex-shrink: 0;
    font-size: 10px;
    font-weight: 700;
    padding: 1px 5px;
    border-radius: 3px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .level-info {
    color: #93c5fd;
    background: rgba(59, 130, 246, 0.15);
  }

  .level-warn {
    color: #fcd34d;
    background: rgba(245, 158, 11, 0.15);
  }

  .level-error {
    color: #fca5a5;
    background: rgba(239, 68, 68, 0.15);
  }

  .log-msg {
    color: #cbd5e1;
    word-break: break-word;
  }

  .log-empty {
    padding: 16px 12px;
    text-align: center;
    color: #475569;
    font-size: 12px;
    font-family: system-ui, sans-serif;
  }
</style>
