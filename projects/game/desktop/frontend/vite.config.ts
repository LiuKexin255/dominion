import { svelte } from '@sveltejs/vite-plugin-svelte'

// Plain config object (no `defineConfig`): the vitest_test runner loads this
// file from bazel runfiles, where a top-level `vite` package is not linked —
// the identity-helper import would fail ESM resolution there. vite build and
// vitest both accept a plain config object.
export default {
  plugins: [svelte()],
}
