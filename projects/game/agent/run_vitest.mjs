import { startVitest } from "vitest/node";

async function main() {
  const args = process.argv.slice(2);
  // startVitest signature is (mode, cliFilters, options, ...). The previous
  // call passed the whole argv array as `mode`, which (a) discarded the file
  // filters so every test ran, and (b) — critically — never surfaced test
  // failures: startVitest only sets a non-zero exitCode on unhandled
  // exceptions / provider errors, never on ordinary assertion failures, and
  // the old `if (!result)` guard was a no-op because `result` is always a
  // truthy Vitest instance. That made every Bazel js_test report green
  // regardless of actual results.
  //
  // Split the vitest command tokens ("run"/"watch") from the file filters,
  // run once (watch disabled — Bazel sandboxes are non-TTY), then exit
  // non-zero when any test failed.
  const filters = args.filter((a) => a !== "run" && a !== "watch");
  const vitest = await startVitest("test", filters, { watch: false });
  const failedTests =
    typeof vitest?.state?.getCountOfFailedTests === "function"
      ? vitest.state.getCountOfFailedTests()
      : 0;
  if (failedTests > 0) {
    process.exit(1);
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
