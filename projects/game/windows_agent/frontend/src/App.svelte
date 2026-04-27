<script lang="ts">
  import ConnectionPanel from './lib/ConnectionPanel.svelte';
  import WindowPanel from './lib/WindowPanel.svelte';
</script>

<main>
  <header>
    <h1>Windows Agent</h1>
  </header>
  <div class="layout">
    <section class="col-left">
      <ConnectionPanel />
    </section>
    <section class="col-right">
      <WindowPanel />
    </section>
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

  .layout {
    display: grid;
    grid-template-columns: 22rem 1fr;
    gap: 1rem;
    padding: 1rem 1.25rem;
    align-items: start;
  }

  @media (max-width: 768px) {
    .layout {
      grid-template-columns: 1fr;
    }
  }
</style>
