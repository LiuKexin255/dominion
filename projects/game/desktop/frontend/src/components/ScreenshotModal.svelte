<script lang="ts">
  let { imageUrl, onClose }: { imageUrl: string; onClose: () => void } = $props()

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') onClose()
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<div class="modal-overlay" onclick={onClose} role="button" tabindex={0}>
  <img class="modal-image" src={imageUrl} alt="Screenshot" />
</div>

<style>
  .modal-overlay {
    position: fixed;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(0, 0, 0, 0.85);
    z-index: 1000;
    cursor: pointer;
  }

  .modal-image {
    max-width: 95vw;
    max-height: 95vh;
    object-fit: contain;
    border-radius: 4px;
  }
</style>
