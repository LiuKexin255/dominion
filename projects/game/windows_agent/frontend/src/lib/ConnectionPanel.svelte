<script lang="ts">
  import { onMount, onDestroy } from 'svelte';

  let connectURL = 'ws://localhost:8080/session';
  let status: AgentStatus = {
    state: 'Disconnected',
    sessionId: '',
    boundWindow: null,
    mediaSegCount: 0,
    lastError: '',
    ffmpegRunning: false,
    helperRunning: false,
    connectedAt: '',
  };
  let connecting = false;

  const connectedStates = new Set(['Connected', 'Bound', 'Streaming']);
  $: isConnected = connectedStates.has(status.state);

  async function handleConnect() {
    if (!connectURL.trim()) return;
    connecting = true;
    try {
      await window.go.main.App.Connect(connectURL.trim());
    } catch (e: any) {
      console.error('Connect failed:', e);
    } finally {
      connecting = false;
    }
  }

  async function handleDisconnect() {
    try {
      await window.go.main.App.Disconnect();
    } catch (e: any) {
      console.error('Disconnect failed:', e);
    }
  }

  function onStatusChanged(s: AgentStatus) {
    status = s;
  }

  onMount(async () => {
    window.runtime.EventsOn('status:changed', onStatusChanged);
    try {
      const s = await window.go.main.App.GetStatus();
      if (s) status = s;
    } catch {
      // GetStatus may fail before Wails is fully initialized
    }
  });

  onDestroy(() => {
    window.runtime.EventsOff('status:changed');
  });
</script>

<div class="panel connection-panel">
  <h2>Connection</h2>

  <div class="status-row">
    <span class="status-label">State:</span>
    <span class="status-dot" class:connected={isConnected} class:error={status.state === 'Error'}></span>
    <span class="status-value">{status.state}</span>
  </div>

  {#if status.sessionId}
    <div class="info-row">
      <span class="info-label">Session:</span>
      <span class="info-value">{status.sessionId}</span>
    </div>
  {/if}

  {#if status.connectedAt}
    <div class="info-row">
      <span class="info-label">Since:</span>
      <span class="info-value">{status.connectedAt}</span>
    </div>
  {/if}

  {#if status.lastError}
    <div class="error-row">{status.lastError}</div>
  {/if}

  <div class="connect-row">
    <input
      type="text"
      bind:value={connectURL}
      placeholder="ws://host:port/session"
      disabled={isConnected || connecting}
    />
    {#if isConnected}
      <button class="btn-disconnect" on:click={handleDisconnect}>Disconnect</button>
    {:else}
      <button class="btn-connect" on:click={handleConnect} disabled={connecting}>
        {connecting ? 'Connecting…' : 'Connect'}
      </button>
    {/if}
  </div>
</div>

<style>
  .connection-panel {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  .status-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }

  .status-dot {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: #64748b;
    flex-shrink: 0;
  }

  .status-dot.connected {
    background: #22c55e;
  }

  .status-dot.error {
    background: #ef4444;
  }

  .status-label {
    color: #94a3b8;
    font-size: 0.85rem;
    min-width: 4rem;
  }

  .status-value {
    font-weight: 600;
    font-size: 0.9rem;
  }

  .info-row {
    display: flex;
    gap: 0.5rem;
    font-size: 0.8rem;
  }

  .info-label {
    color: #94a3b8;
    min-width: 4rem;
  }

  .info-value {
    color: #cbd5e1;
    word-break: break-all;
  }

  .error-row {
    color: #fca5a5;
    background: rgba(239, 68, 68, 0.15);
    padding: 0.4rem 0.6rem;
    border-radius: 4px;
    font-size: 0.8rem;
    word-break: break-word;
  }

  .connect-row {
    display: flex;
    gap: 0.5rem;
    margin-top: 0.25rem;
  }

  .connect-row input {
    flex: 1;
    background: #1e293b;
    border: 1px solid #334155;
    border-radius: 4px;
    color: #e2e8f0;
    padding: 0.4rem 0.6rem;
    font-size: 0.85rem;
    font-family: monospace;
  }

  .connect-row input:disabled {
    opacity: 0.5;
  }

  .connect-row input:focus {
    outline: none;
    border-color: #3b82f6;
  }

  button {
    padding: 0.4rem 1rem;
    border: none;
    border-radius: 4px;
    font-size: 0.85rem;
    cursor: pointer;
    font-weight: 600;
    transition: opacity 0.15s;
  }

  button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .btn-connect {
    background: #2563eb;
    color: #fff;
  }

  .btn-connect:hover:not(:disabled) {
    background: #1d4ed8;
  }

  .btn-disconnect {
    background: #991b1b;
    color: #fca5a5;
  }

  .btn-disconnect:hover:not(:disabled) {
    background: #7f1d1d;
  }
</style>
