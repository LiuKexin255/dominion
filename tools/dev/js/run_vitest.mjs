// Canonical shared vitest test-runner shim for all Bazel `js_test` targets.
// Single source of truth: replaces the six drifting per-package copies
// (`projects/game/agent/run_vitest.mjs` + five under `common/js/...`), five of
// which still carry the original bug where `if (!result)` never fired because
// `startVitest` always returns a truthy `Vitest` instance.
//
// Exit-code contract:
//   specs/019-js-test-reliability/contracts/run-vitest-shim.md
// API rationale (why `getCountOfFailedTests()` is required, why `startVitest`
// alone does not surface assertion failures): research.md §1
//   specs/019-js-test-reliability/research.md
// vitest `startVitest` (3.x signature: `(mode, cliFilters, options, viteOverrides?, vitestOptions?)`):
//   https://vitest.dev/guide/advanced/tests
import { startVitest } from "vitest/node";

async function main() {
  const filters = process.argv.slice(2).filter((a) => a !== "run" && a !== "watch");
  const vitest = await startVitest("test", filters, { watch: false });
  // FR-004: await full teardown before reading the result so the exit does not
  // race with asynchronous reporters/cleanup.
  await vitest.close();
  // Fail-closed (FR-001): if the failed-count reader is missing or not callable,
  // treat the run as failed rather than silently passing with a default of 0.
  const read = vitest?.state?.getCountOfFailedTests;
  const failed = typeof read === "function" ? read.call(vitest.state) : Number.POSITIVE_INFINITY;
  process.exit(failed > 0 ? 1 : 0);
}

main().catch((err) => {
  // Top-level / unhandled exception (e.g. import error) -> FAILED (Edge Case).
  console.error(err);
  process.exit(1);
});
