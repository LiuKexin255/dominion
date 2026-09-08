import { describe, it, expect, vi } from "vitest";
import { createDaemon, type Worker } from "./daemon.js";
import { Stage } from "./component.js";

function neverAborted(): AbortSignal {
  return new AbortController().signal;
}

function flushTimers(ms = 5): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

interface WorkerOptions {
  error?: unknown;
  /** run blocks until the supervision signal aborts, then rejects with its reason. */
  hangUntilAbort?: boolean;
}

function fakeWorker(opts: WorkerOptions = {}): { worker: Worker; run: ReturnType<typeof vi.fn> } {
  const run = vi.fn((signal: AbortSignal): Promise<void> => {
    if (opts.hangUntilAbort) {
      return new Promise<void>((_resolve, reject) => {
        signal.addEventListener("abort", () => reject(signal.reason), { once: true });
      });
    }
    if (opts.error !== undefined) return Promise.reject(opts.error);
    return Promise.resolve();
  });
  return { worker: { run }, run };
}

describe("createDaemon", () => {
  it("is a Daemon-stage component exposing exited", () => {
    const { worker } = fakeWorker();
    const d = createDaemon("d", () => worker);
    expect(d.stage).toBe(Stage.Daemon);
    expect("exited" in d).toBe(true);
  });

  it("retries after build failures until a build succeeds", async () => {
    let builds = 0;
    const { worker, run } = fakeWorker({ hangUntilAbort: true });
    const d = createDaemon(
      "d",
      () => {
        builds += 1;
        if (builds < 3) throw new Error("build boom");
        return worker;
      },
      { initialBackoffMs: 5, maxBackoffMs: 10 },
    );
    await d.start(neverAborted());
    await vi.waitFor(() => expect(builds).toBe(3));

    await d.stop(neverAborted());
    expect(builds).toBe(3);
    // The successful third build launched the worker exactly once; the two
    // failed builds never reached a run.
    expect(run).toHaveBeenCalledOnce();
  });

  it("stops without fatal when a build fails with an AbortError during shutdown", async () => {
    // buildWorker hangs until the supervision signal aborts and then fails
    // with the AbortError reason — the shutdown-window build failure must
    // end supervision with neither a restart nor a fatal report.
    let builds = 0;
    const d = createDaemon("d", (signal: AbortSignal) => {
      builds += 1;
      return new Promise<Worker>((_resolve, reject) => {
        signal.addEventListener("abort", () => reject(signal.reason), { once: true });
      });
    });
    await d.start(neverAborted());

    let exitedSettled = false;
    void d.exited.then(
      () => {
        exitedSettled = true;
      },
      () => {
        exitedSettled = true;
      },
    );

    await d.stop(neverAborted());
    await flushTimers();
    expect(builds).toBe(1);
    expect(exitedSettled).toBe(false);
  });

  it("does not report fatal when a non-AbortError build failure races the shutdown", async () => {
    // A plain Error classifies as "restart", and with maxRestarts: 0 the
    // restart budget is exhausted immediately — escalation to fatal is only
    // prevented by the shutdown-window early return in the build-failure
    // path, which this case locks in.
    let builds = 0;
    const d = createDaemon(
      "d",
      (signal: AbortSignal) => {
        builds += 1;
        return new Promise<Worker>((_resolve, reject) => {
          signal.addEventListener("abort", () => reject(new Error("boom")), { once: true });
        });
      },
      { maxRestarts: 0 },
    );
    await d.start(neverAborted());

    let exitedSettled = false;
    void d.exited.then(
      () => {
        exitedSettled = true;
      },
      () => {
        exitedSettled = true;
      },
    );

    await d.stop(neverAborted());
    await flushTimers();
    expect(builds).toBe(1);
    expect(exitedSettled).toBe(false);
  });

  it("restarts with exponential backoff min(cur*2, max)", async () => {
    const runTimes: number[] = [];
    const d = createDaemon(
      "d",
      () => ({
        run: () => {
          runTimes.push(Date.now());
          return Promise.reject(new Error("boom"));
        },
      }),
      { initialBackoffMs: 10, maxBackoffMs: 30 },
    );
    await d.start(neverAborted());
    await vi.waitFor(() => expect(runTimes.length).toBeGreaterThanOrEqual(4));

    // Lower bounds prove the delays: 10, then min(20,30)=20, then
    // min(40,30)=30 (small slack for timer precision).
    const gaps = runTimes.slice(1).map((t, i) => t - runTimes[i]);
    expect(gaps[0]).toBeGreaterThanOrEqual(8);
    expect(gaps[1]).toBeGreaterThanOrEqual(18);
    expect(gaps[2]).toBeGreaterThanOrEqual(28);

    await d.stop(neverAborted());
  });

  it("reports fatal via exited when maxRestarts is exhausted", async () => {
    const { worker, run } = fakeWorker({ error: new Error("crash") });
    const d = createDaemon("d", () => worker, {
      initialBackoffMs: 5,
      maxBackoffMs: 10,
      maxRestarts: 2,
    });
    await d.start(neverAborted());

    await expect(d.exited).resolves.toBeInstanceOf(Error);
    // One initial run plus two restarts; the third failure is fatal.
    expect(run).toHaveBeenCalledTimes(3);

    await d.stop(neverAborted());
  });

  it("does not restart when the supervision signal cancels the worker", async () => {
    const { worker, run } = fakeWorker({ hangUntilAbort: true });
    const d = createDaemon("d", () => worker);
    await d.start(neverAborted());
    await vi.waitFor(() => expect(run).toHaveBeenCalledOnce());

    let exitedSettled = false;
    void d.exited.then(
      () => {
        exitedSettled = true;
      },
      () => {
        exitedSettled = true;
      },
    );

    await d.stop(neverAborted());
    await flushTimers();
    expect(run).toHaveBeenCalledOnce();
    expect(exitedSettled).toBe(false);
  });

  it("does not restart nor report fatal when the worker finishes cleanly", async () => {
    const { worker, run } = fakeWorker();
    const d = createDaemon("d", () => worker);
    await d.start(neverAborted());
    await vi.waitFor(() => expect(run).toHaveBeenCalledOnce());

    await d.stop(neverAborted());
    await flushTimers();
    expect(run).toHaveBeenCalledOnce();
    let exitedSettled = false;
    void d.exited.then(
      () => {
        exitedSettled = true;
      },
      () => {
        exitedSettled = true;
      },
    );
    await flushTimers();
    expect(exitedSettled).toBe(false);
  });

  it("honors a custom classifyError", async () => {
    const { worker, run } = fakeWorker({ error: new Error("custom") });
    const classify = vi.fn(() => "fatal" as const);
    const d = createDaemon("d", () => worker, { classifyError: classify });
    await d.start(neverAborted());

    await expect(d.exited).resolves.toBeInstanceOf(Error);
    expect(classify).toHaveBeenCalled();
    expect(run).toHaveBeenCalledOnce();

    await d.stop(neverAborted());
  });
});
