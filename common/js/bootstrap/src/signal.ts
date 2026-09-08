/**
 * Internal AbortSignal helpers shared by the health endpoint and the
 * Bootstrap stop sequence (not part of the public barrel).
 */

/**
 * Resolves when `signal` aborts (immediately when already aborted).
 */
export function waitForAbort(signal: AbortSignal): Promise<void> {
  if (signal.aborted) return Promise.resolve();
  return new Promise((resolve) => {
    signal.addEventListener("abort", () => resolve(), { once: true });
  });
}

/**
 * Awaits `op` unless `signal` aborts first, in which case the returned
 * promise rejects with `onAbort(signal.reason)`.
 *
 * The losing side's rejection is consumed here so a budget timeout can
 * never surface as an unhandled rejection after the race settled.
 */
export function raceAbort(
  op: Promise<void>,
  signal: AbortSignal,
  onAbort: (reason: unknown) => Error,
): Promise<void> {
  return new Promise<void>((resolve, reject) => {
    if (signal.aborted) {
      // Consume op's rejection: it lost the race before any handler below
      // could observe it, and an unconsumed rejection would crash the
      // process under Node's default --unhandled-rejections=throw.
      op.catch(() => {});
      reject(onAbort(signal.reason));
      return;
    }
    const onAbortListener = () => reject(onAbort(signal.reason));
    signal.addEventListener("abort", onAbortListener, { once: true });
    const cleanup = () => signal.removeEventListener("abort", onAbortListener);
    op.then(
      () => {
        cleanup();
        resolve();
      },
      (err: unknown) => {
        cleanup();
        reject(err);
      },
    );
  });
}
