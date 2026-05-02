<script lang="ts">
  import { onMount, onDestroy } from 'svelte';

  interface LogItem {
    timestamp: string;
    level: string;
    module: string;
    message: string;
    fields: Record<string, string>;
  }

  const MAX_ENTRIES = 500;
  const HARD_LIMIT = 1000;

  let entries: LogItem[] = [];
  let filter: 'ALL' | 'INFO' | 'WARN' | 'ERROR' = 'ALL';
  let autoScroll = true;
  let expanded: Set<number> = new Set();
  let container: HTMLDivElement;

  $: filtered = filter === 'ALL' ? entries : entries.filter(e => e.level.toUpperCase() === filter);

  function onLogEntry(data: any) {
    let entry: LogItem;
    if (data && data.timestamp !== undefined) {
      entry = {
        timestamp: data.timestamp,
        level: data.level ?? 'info',
        module: data.module ?? '',
        message: data.message ?? '',
        fields: data.fields ?? {},
      };
    } else {
      entry = {
        timestamp: new Date().toISOString(),
        level: data?.level ?? 'info',
        module: '',
        message: data?.message ?? '',
        fields: {},
      };
    }
    entries = [...entries, entry];
    if (entries.length > HARD_LIMIT) {
      entries = entries.slice(entries.length - MAX_ENTRIES);
    }
  }

  function clear() {
    entries = [];
    expanded = new Set();
  }

  function toggleExpand(idx: number) {
    const next = new Set(expanded);
    if (next.has(idx)) {
      next.delete(idx);
    } else {
      next.add(idx);
    }
    expanded = next;
  }

  function levelColor(level: string): string {
    switch (level.toUpperCase()) {
      case 'ERROR': return '#ef4444';
      case 'WARN': return '#f59e0b';
      case 'INFO': return '#22c55e';
      default: return '#94a3b8';
    }
  }

  function formatTime(iso: string): string {
    if (!iso) return '';
    const d = new Date(iso);
    return d.toLocaleTimeString('zh-CN', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' });
  }

  function scrollToBottom() {
    if (container) {
      container.scrollTop = container.scrollHeight;
    }
  }

  let prevFilteredLength = 0;
  $: if (autoScroll && container && filtered.length > prevFilteredLength) {
    prevFilteredLength = filtered.length;
    requestAnimationFrame(scrollToBottom);
  }

  onMount(() => {
    const wails = window.runtime;
    if (wails?.EventsOn) {
      wails.EventsOn('log:entry', onLogEntry);
    }
  });

  onDestroy(() => {
    const wails = window.runtime;
    if (wails?.EventsOff) {
      wails.EventsOff('log:entry');
    }
  });
</script>

<div class="panel log-panel">
  <div class="log-toolbar">
    <div class="filter-group">
      <button class:active={filter === 'ALL'} on:click={() => filter = 'ALL'}>全部</button>
      <button class:active={filter === 'INFO'} on:click={() => filter = 'INFO'}>INFO</button>
      <button class:active={filter === 'WARN'} on:click={() => filter = 'WARN'}>WARN</button>
      <button class:active={filter === 'ERROR'} on:click={() => filter = 'ERROR'}>ERROR</button>
    </div>
    <div class="toolbar-actions">
      <label class="auto-scroll"><input type="checkbox" bind:checked={autoScroll} />自动滚动</label>
      <button on:click={clear}>清空</button>
    </div>
  </div>

  <div class="log-entries" bind:this={container}>
    {#each filtered as entry, i (i)}
      <div class="log-entry">
        <span class="log-time">{formatTime(entry.timestamp)}</span>
        <span class="log-level" style="color:{levelColor(entry.level)}">[{entry.level.toUpperCase()}]</span>
        {#if entry.module}
          <span class="log-module">{entry.module}</span>
        {/if}
        <span class="log-message" on:click={() => toggleExpand(i)}>{entry.message}</span>
        {#if expanded.has(i) && Object.keys(entry.fields).length > 0}
          <div class="log-fields">
            {#each Object.entries(entry.fields) as [key, value]}
              <div class="field"><span class="field-key">{key}</span>=<span class="field-value">{value}</span></div>
            {/each}
          </div>
        {/if}
      </div>
    {:else}
      <div class="empty-logs">暂无日志</div>
    {/each}
  </div>
</div>

<style>
  .log-panel {
    display: flex;
    flex-direction: column;
    height: 100%;
    padding: 0;
    overflow: hidden;
  }

  .log-toolbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 0.4rem 0.75rem;
    border-bottom: 1px solid #334155;
    flex-wrap: wrap;
    gap: 0.4rem;
    flex-shrink: 0;
  }

  .filter-group {
    display: flex;
    gap: 0.25rem;
  }

  .filter-group button {
    font-family: inherit;
    font-size: 0.7rem;
    padding: 0.2rem 0.5rem;
    border-radius: 3px;
    border: 1px solid #475569;
    background: #1e293b;
    color: #94a3b8;
    cursor: pointer;
  }

  .filter-group button.active {
    background: #334155;
    color: #e2e8f0;
    border-color: #38bdf8;
  }

  .toolbar-actions {
    display: flex;
    gap: 0.5rem;
    align-items: center;
  }

  .toolbar-actions button {
    font-family: inherit;
    font-size: 0.7rem;
    padding: 0.2rem 0.5rem;
    border-radius: 3px;
    border: 1px solid #475569;
    background: #1e293b;
    color: #94a3b8;
    cursor: pointer;
  }

  .toolbar-actions button:hover {
    background: #334155;
    color: #e2e8f0;
  }

  .auto-scroll {
    font-size: 0.72rem;
    color: #94a3b8;
    display: flex;
    align-items: center;
    gap: 0.25rem;
    cursor: pointer;
  }

  .auto-scroll input {
    margin: 0;
    accent-color: #38bdf8;
  }

  .log-entries {
    flex: 1;
    overflow: auto;
    padding: 0.4rem 0.75rem;
    font-family: 'Courier New', monospace;
    font-size: 0.72rem;
    line-height: 1.6;
  }

  .log-entry {
    padding: 0.1rem 0;
    border-left: 3px solid transparent;
    padding-left: 0.5rem;
    border-bottom: 1px solid rgba(255, 255, 255, 0.03);
  }

  .log-entry:has(.log-level[style*="#ef4444"]) {
    border-left-color: #ef4444;
  }

  .log-entry:has(.log-level[style*="#f59e0b"]) {
    border-left-color: #f59e0b;
  }

  .log-entry:has(.log-level[style*="#22c55e"]) {
    border-left-color: #22c55e;
  }

  .log-time {
    color: #64748b;
    margin-right: 0.4rem;
  }

  .log-level {
    font-weight: 600;
    margin-right: 0.3rem;
  }

  .log-module {
    color: #38bdf8;
    margin-right: 0.3rem;
  }

  .log-message {
    color: #cbd5e1;
    cursor: pointer;
  }

  .log-message:hover {
    text-decoration: underline;
  }

  .log-fields {
    margin: 0.15rem 0 0.15rem 0.5rem;
    padding-left: 0.5rem;
    border-left: 1px solid #334155;
  }

  .field {
    font-size: 0.68rem;
    color: #64748b;
  }

  .field-key {
    color: #94a3b8;
  }

  .field-value {
    color: #cbd5e1;
    margin-left: 0.2rem;
  }

  .empty-logs {
    color: #64748b;
    text-align: center;
    padding: 1rem;
  }
</style>
