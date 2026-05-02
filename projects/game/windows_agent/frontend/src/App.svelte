<script lang="ts">
  import ConnectionPanel from './lib/ConnectionPanel.svelte';
  import WindowPanel from './lib/WindowPanel.svelte';
  import DebugPanel from './lib/DebugPanel.svelte';
  import LogPanel from './lib/LogPanel.svelte';

  let activeTab = 'connection';
</script>

<main>
  <header>
    <h1>Windows Agent</h1>
  </header>

  <!-- Upper: Main tabs -->
  <div class="main-area">
    <div class="tab-bar">
      <button class="tab" class:active={activeTab === 'connection'} on:click={() => activeTab = 'connection'}>连接</button>
      <button class="tab" class:active={activeTab === 'window'} on:click={() => activeTab = 'window'}>窗口捕获</button>
      <button class="tab" class:active={activeTab === 'debug'} on:click={() => activeTab = 'debug'}>调试</button>
    </div>
    <div class="tab-content">
      {#if activeTab === 'connection'}<ConnectionPanel />{/if}
      {#if activeTab === 'window'}<WindowPanel />{/if}
      {#if activeTab === 'debug'}<DebugPanel />{/if}
    </div>
  </div>

  <!-- Lower: Log -->
  <div class="log-area">
    <div class="log-header"><span>日志</span></div>
    <div class="log-content"><LogPanel /></div>
  </div>
</main>

<style>
  main {
    display: flex;
    flex-direction: column;
    min-height: 100vh;
    padding: 0;
  }

  header {
    padding: 0.75rem 1.25rem;
    border-bottom: 1px solid #1e293b;
  }

  header h1 {
    margin: 0;
    font-size: 1.1rem;
    font-weight: 700;
    color: #e2e8f0;
  }

  .main-area {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .tab-bar {
    display: flex;
    gap: 0;
    padding: 0 1.25rem;
    border-bottom: 1px solid #1e293b;
    background: #0f172a;
  }

  .tab {
    padding: 0.6rem 1rem;
    font-size: 0.85rem;
    font-weight: 600;
    color: #94a3b8;
    background: none;
    border: none;
    border-bottom: 2px solid transparent;
    cursor: pointer;
    transition: color 0.15s, border-color 0.15s;
  }

  .tab:hover {
    color: #e2e8f0;
    background: rgba(255, 255, 255, 0.05);
  }

  .tab.active {
    color: #e2e8f0;
    border-bottom-color: #38bdf8;
  }

  .tab-content {
    flex: 1;
    overflow: auto;
    padding: 1rem 1.25rem;
  }

  .log-area {
    border-top: 1px solid #334155;
    min-height: 200px;
    max-height: 350px;
    display: flex;
    flex-direction: column;
    background: #0f172a;
  }

  .log-header {
    padding: 0.5rem 1.25rem;
    border-bottom: 1px solid #1e293b;
    font-size: 0.85rem;
    font-weight: 600;
    color: #e2e8f0;
  }

  .log-content {
    flex: 1;
    overflow: auto;
    padding: 0;
  }
</style>
