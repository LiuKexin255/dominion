<script lang="ts">
  import { onMount, onDestroy } from 'svelte';

  let windows: WindowInfo[] = [];
  let loading = false;
  let boundWindow: WindowRef | null = null;

  async function refresh() {
    loading = true;
    try {
      const result = await window.go.main.App.EnumerateWindows();
      windows = result ?? [];
    } catch (e: any) {
      console.error('EnumerateWindows failed:', e);
    } finally {
      loading = false;
    }
  }

  async function bind(hwnd: number) {
    try {
      await window.go.main.App.BindWindow(hwnd);
    } catch (e: any) {
      console.error('BindWindow failed:', e);
    }
  }

  function onWindowList(list: WindowInfo[]) {
    windows = list ?? [];
  }

  function onStatusChanged(s: AgentStatus) {
    boundWindow = s.boundWindow ?? null;
  }

  onMount(async () => {
    window.runtime.EventsOn('window:list', onWindowList);
    window.runtime.EventsOn('status:changed', onStatusChanged);
    try {
      const s = await window.go.main.App.GetStatus();
      if (s?.boundWindow) boundWindow = s.boundWindow;
    } catch {
      // GetStatus may fail before Wails is fully initialized
    }
    await refresh();
  });

  onDestroy(() => {
    window.runtime.EventsOff('window:list');
    window.runtime.EventsOff('status:changed');
  });
</script>

<div class="panel window-panel">
  <div class="panel-header">
    <h2>Windows</h2>
    <button class="btn-refresh" on:click={refresh} disabled={loading}>
      {loading ? 'Loading…' : 'Refresh'}
    </button>
  </div>

  {#if boundWindow}
    <div class="bound-info">
      <span class="bound-label">Bound:</span>
      <span class="bound-title">{boundWindow.title || '(untitled)'}</span>
      <span class="bound-hwnd">0x{boundWindow.hwnd.toString(16).toUpperCase()}</span>
    </div>
  {/if}

  <div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th class="col-hwnd">HWND</th>
          <th class="col-title">Title</th>
          <th class="col-class">Class</th>
          <th class="col-action"></th>
        </tr>
      </thead>
      <tbody>
        {#if windows.length === 0}
          <tr>
            <td colspan="4" class="empty">No windows found</td>
          </tr>
        {:else}
          {#each windows as w}
            <tr>
              <td class="mono">0x{w.HWND.toString(16).toUpperCase()}</td>
              <td title={w.Title}>{w.Title || '(untitled)'}</td>
              <td class="mono" title={w.ClassName}>{w.ClassName}</td>
              <td>
                <button
                  class="btn-bind"
                  on:click={() => bind(w.HWND)}
                  disabled={boundWindow?.hwnd === w.HWND}
                >
                  {boundWindow?.hwnd === w.HWND ? 'Bound' : 'Bind'}
                </button>
              </td>
            </tr>
          {/each}
        {/if}
      </tbody>
    </table>
  </div>
</div>

<style>
  .window-panel {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  .panel-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .btn-refresh {
    padding: 0.3rem 0.75rem;
    background: #334155;
    color: #e2e8f0;
    border: none;
    border-radius: 4px;
    font-size: 0.8rem;
    cursor: pointer;
    font-weight: 600;
  }

  .btn-refresh:hover:not(:disabled) {
    background: #475569;
  }

  .btn-refresh:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .bound-info {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    background: rgba(34, 197, 94, 0.1);
    border: 1px solid rgba(34, 197, 94, 0.3);
    border-radius: 4px;
    padding: 0.4rem 0.6rem;
    font-size: 0.8rem;
  }

  .bound-label {
    color: #22c55e;
    font-weight: 600;
  }

  .bound-title {
    color: #e2e8f0;
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .bound-hwnd {
    color: #94a3b8;
    font-family: monospace;
    font-size: 0.75rem;
  }

  .table-wrap {
    overflow-x: auto;
  }

  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.8rem;
  }

  thead th {
    text-align: left;
    color: #94a3b8;
    font-weight: 600;
    padding: 0.4rem 0.5rem;
    border-bottom: 1px solid #1e293b;
    font-size: 0.75rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  tbody td {
    padding: 0.35rem 0.5rem;
    border-bottom: 1px solid #1e293b;
    color: #cbd5e1;
    max-width: 12rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .col-hwnd { width: 7rem; }
  .col-class { width: 8rem; }
  .col-action { width: 4.5rem; }

  .mono {
    font-family: monospace;
    font-size: 0.75rem;
  }

  .empty {
    text-align: center;
    color: #64748b;
    padding: 1rem !important;
  }

  .btn-bind {
    padding: 0.2rem 0.6rem;
    background: #1e3a5f;
    color: #93c5fd;
    border: 1px solid #2563eb;
    border-radius: 3px;
    font-size: 0.75rem;
    cursor: pointer;
    font-weight: 600;
  }

  .btn-bind:hover:not(:disabled) {
    background: #1e40af;
    color: #dbeafe;
  }

  .btn-bind:disabled {
    background: #14532d;
    color: #86efac;
    border-color: #22c55e;
    cursor: default;
  }
</style>
