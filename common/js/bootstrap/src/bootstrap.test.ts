import { describe, it, expect, vi } from "vitest";
import { Bootstrap } from "./bootstrap.js";
import { Stage, type Component, type ExitWatchable } from "./component.js";
import type { HealthService } from "./health.js";

// Recording test doubles wired through the DI seams (healthServerFactory /
// plain-object components), per the mock convention in style/javascript.md:
// every intercepted call is asserted positively via the shared order log.

interface ComponentOptions {
  startError?: Error;
  stopError?: Error;
  /** stop records the call but never settles (unresponsive to the budget signal). */
  stopHangs?: boolean;
}

function fakeComponent(
  order: string[],
  name: string,
  stage: Component["stage"],
  opts: ComponentOptions = {},
): Component {
  return {
    name,
    stage,
    start: vi.fn(async () => {
      order.push(`start:${name}`);
      if (opts.startError) throw opts.startError;
    }),
    stop: vi.fn(async () => {
      order.push(`stop:${name}`);
      if (opts.stopHangs) return new Promise<void>(() => {});
      if (opts.stopError) throw opts.stopError;
    }),
  };
}

function fakeHealth(
  order: string[],
  opts: { startError?: Error } = {},
): { service: HealthService; start: ReturnType<typeof vi.fn>; stop: ReturnType<typeof vi.fn> } {
  const start = vi.fn(async () => {
    order.push("start:health");
    if (opts.startError) throw opts.startError;
  });
  const stop = vi.fn(async () => {
    order.push("stop:health");
  });
  return { service: { start, stop }, start, stop };
}

/** Waits until the bootstrap reached the running state (health started). */
function waitForRunning(order: string[]): Promise<void> {
  return vi.waitFor(() => expect(order).toContain("start:health"));
}

function rejectionOf(p: Promise<void>): Promise<unknown> {
  return p.then(
    () => null,
    (err) => err,
  );
}

describe("Bootstrap", () => {
  it("starts components in stage asc then name asc order", async () => {
    const order: string[] = [];
    const health = fakeHealth(order);
    const b = new Bootstrap({ healthServerFactory: () => health.service });
    b.register(fakeComponent(order, "zeta", Stage.Server));
    b.register(fakeComponent(order, "alpha", Stage.Foundation));
    b.register(fakeComponent(order, "beta", Stage.Foundation));
    b.register(fakeComponent(order, "delta", Stage.Client));

    const controller = new AbortController();
    const done = b.run({ signal: controller.signal });
    await waitForRunning(order);
    controller.abort();
    await expect(done).resolves.toBeUndefined();

    expect(order.filter((entry) => entry.startsWith("start:"))).toEqual([
      "start:alpha",
      "start:beta",
      "start:delta",
      "start:zeta",
      "start:health",
    ]);
  });

  it("rejects a duplicate component name", () => {
    const b = new Bootstrap();
    b.register(fakeComponent([], "dup", Stage.Client));
    expect(() => b.register(fakeComponent([], "dup", Stage.Server)))
      .toThrowError(/duplicate component name "dup"/);
  });

  it("rejects register after run started", async () => {
    const order: string[] = [];
    const health = fakeHealth(order);
    const b = new Bootstrap({ healthServerFactory: () => health.service });
    b.register(fakeComponent(order, "c", Stage.Client));

    const controller = new AbortController();
    const done = b.run({ signal: controller.signal });
    await waitForRunning(order);
    expect(() => b.register(fakeComponent(order, "late", Stage.Client)))
      .toThrowError(/after run has been called/);
    controller.abort();

    await expect(done).resolves.toBeUndefined();
  });

  it("rejects a second run call", async () => {
    const order: string[] = [];
    const health = fakeHealth(order);
    const b = new Bootstrap({ healthServerFactory: () => health.service });
    b.register(fakeComponent(order, "c", Stage.Client));

    const controller = new AbortController();
    const done = b.run({ signal: controller.signal });
    await waitForRunning(order);
    await expect(b.run({ signal: controller.signal }))
      .rejects.toThrowError(/run has already been called/);
    controller.abort();

    await expect(done).resolves.toBeUndefined();
  });

  it("rolls back started components in reverse order when a start fails", async () => {
    const order: string[] = [];
    const health = fakeHealth(order);
    const healthFactory = vi.fn(() => health.service);
    const b = new Bootstrap({ healthServerFactory: healthFactory });
    b.register(fakeComponent(order, "first", Stage.Foundation));
    b.register(fakeComponent(order, "second", Stage.Client, { startError: new Error("boom") }));
    b.register(fakeComponent(order, "third", Stage.Server));

    const err = await rejectionOf(b.run({ signal: new AbortController().signal }));

    expect(healthFactory).not.toHaveBeenCalled();
    expect(err).toBeInstanceOf(AggregateError);
    expect((err as AggregateError).errors[0].message).toContain('component "second" failed to start');
    // The failing component gets no stop; the unstarted component is untouched;
    // health never started, so no listener residue.
    expect(order).toEqual(["start:first", "start:second", "stop:first"]);
  });

  it("starts health last and stops it first (FIFO)", async () => {
    const order: string[] = [];
    const health = fakeHealth(order);
    const healthFactory = vi.fn(() => health.service);
    const b = new Bootstrap({ healthServerFactory: healthFactory });
    b.register(fakeComponent(order, "otel", Stage.Foundation));
    b.register(fakeComponent(order, "server", Stage.Server));

    const controller = new AbortController();
    const done = b.run({ signal: controller.signal });
    await waitForRunning(order);
    controller.abort();
    await expect(done).resolves.toBeUndefined();

    expect(healthFactory).toHaveBeenCalledOnce();
    expect(health.start).toHaveBeenCalledOnce();
    expect(health.stop).toHaveBeenCalledOnce();
    expect(order).toEqual([
      "start:otel",
      "start:server",
      "start:health",
      "stop:health",
      "stop:server",
      "stop:otel",
    ]);
  });

  it("rolls back components when the health server fails to start", async () => {
    const order: string[] = [];
    const health = fakeHealth(order, { startError: new Error("port taken") });
    const b = new Bootstrap({ healthServerFactory: () => health.service });
    b.register(fakeComponent(order, "c1", Stage.Foundation));
    b.register(fakeComponent(order, "c2", Stage.Server));

    const err = await rejectionOf(b.run({ signal: new AbortController().signal }));

    expect(err).toBeInstanceOf(AggregateError);
    expect((err as Error).message).toContain("health server failed to start");
    expect((err as AggregateError).errors[0].message).toContain("port taken");
    expect(order).toEqual([
      "start:c1",
      "start:c2",
      "start:health",
      "stop:c2",
      "stop:c1",
    ]);
  });

  it("aggregates stops that do not finish within the shutdown budget", async () => {
    const order: string[] = [];
    const health = fakeHealth(order);
    const b = new Bootstrap({ healthServerFactory: () => health.service, shutdownTimeoutMs: 50 });
    b.register(fakeComponent(order, "after", Stage.Client));
    b.register(fakeComponent(order, "slow", Stage.Server, { stopHangs: true }));

    const controller = new AbortController();
    const done = b.run({ signal: controller.signal });
    await waitForRunning(order);
    controller.abort();
    const err = await rejectionOf(done);

    expect(err).toBeInstanceOf(AggregateError);
    expect((err as AggregateError).errors).toHaveLength(2);
    expect((err as AggregateError).errors[0].message).toContain('"slow"');
    expect((err as AggregateError).errors[1].message).toContain('"after"');
    // Every target was still attempted against the shared budget signal.
    expect(order).toEqual([
      "start:after",
      "start:slow",
      "start:health",
      "stop:health",
      "stop:slow",
      "stop:after",
    ]);
  });

  it("aggregates stop errors and still stops every component", async () => {
    const order: string[] = [];
    const health = fakeHealth(order);
    const b = new Bootstrap({ healthServerFactory: () => health.service });
    b.register(fakeComponent(order, "c1", Stage.Foundation, { stopError: new Error("stop err 1") }));
    b.register(fakeComponent(order, "c2", Stage.Server, { stopError: new Error("stop err 2") }));

    const controller = new AbortController();
    const done = b.run({ signal: controller.signal });
    await waitForRunning(order);
    controller.abort();
    const err = await rejectionOf(done);

    expect(err).toBeInstanceOf(AggregateError);
    expect((err as AggregateError).errors).toHaveLength(2);
    expect((err as AggregateError).errors[0].message).toContain('"c2"');
    expect((err as AggregateError).errors[1].message).toContain('"c1"');
    expect(order).toEqual([
      "start:c1",
      "start:c2",
      "start:health",
      "stop:health",
      "stop:c2",
      "stop:c1",
    ]);
  });

  it("resolves on an injected AbortSignal abort (clean shutdown)", async () => {
    const order: string[] = [];
    const health = fakeHealth(order);
    const b = new Bootstrap({ healthServerFactory: () => health.service });
    b.register(fakeComponent(order, "c", Stage.Client));

    const controller = new AbortController();
    const done = b.run({ signal: controller.signal });
    await waitForRunning(order);
    controller.abort();
    await expect(done).resolves.toBeUndefined();

    expect(order).toEqual(["start:c", "start:health", "stop:health", "stop:c"]);
  });

  it("shuts down globally and rejects when a component exits with an error", async () => {
    const order: string[] = [];
    const health = fakeHealth(order);
    const b = new Bootstrap({ healthServerFactory: () => health.service });
    let releaseExit: (err: Error | undefined) => void = () => {};
    const exited = new Promise<Error | undefined>((resolve) => {
      releaseExit = resolve;
    });
    const daemon: Component & ExitWatchable = {
      ...fakeComponent(order, "daemon", Stage.Daemon),
      exited,
    };
    b.register(daemon);
    b.register(fakeComponent(order, "server", Stage.Server));

    // signals: [] keeps the run waiting purely on the injected exit signal.
    const done = b.run({ signals: [] });
    await vi.waitFor(() => expect(order).toContain("start:health"));
    releaseExit(new Error("daemon crashed"));

    const err = await rejectionOf(done);
    expect(err).toBeInstanceOf(AggregateError);
    expect((err as AggregateError).errors[0].message).toContain("daemon crashed");
    expect(order).toEqual([
      "start:daemon",
      "start:server",
      "start:health",
      "stop:health",
      "stop:server",
      "stop:daemon",
    ]);
  });

  it("removes process signal listeners after run finishes", async () => {
    const order: string[] = [];
    const health = fakeHealth(order);
    const b = new Bootstrap({ healthServerFactory: () => health.service });
    b.register(fakeComponent(order, "c", Stage.Client));

    // Uncommon signal + RunOptions.signals injection, same strategy as Go
    // bootstrap_test.go's SIGUSR1: never touches SIGTERM/SIGINT handling of
    // the surrounding test runner.
    const before = process.listenerCount("SIGUSR1");
    const done = b.run({ signals: ["SIGUSR1"] });
    await waitForRunning(order);
    expect(process.listenerCount("SIGUSR1")).toBe(before + 1);

    process.kill(process.pid, "SIGUSR1");
    await expect(done).resolves.toBeUndefined();
    expect(process.listenerCount("SIGUSR1")).toBe(before);
  });

  it("keeps running when a component exits normally and stops on the injected signal", async () => {
    const order: string[] = [];
    const health = fakeHealth(order);
    const b = new Bootstrap({ healthServerFactory: () => health.service });
    let releaseExit: (err: Error | undefined) => void = () => {};
    const exited = new Promise<Error | undefined>((resolve) => {
      releaseExit = resolve;
    });
    const daemon: Component & ExitWatchable = {
      ...fakeComponent(order, "daemon", Stage.Daemon),
      exited,
    };
    b.register(daemon);

    const controller = new AbortController();
    const done = b.run({ signals: [], signal: controller.signal });
    await waitForRunning(order);

    // A normal exit (resolve without an Error) must not trigger the global
    // shutdown: give any wrongful stop sequence time to surface, then assert
    // nothing stopped.
    releaseExit(undefined);
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(order).not.toContain("stop:health");

    controller.abort();
    await expect(done).resolves.toBeUndefined();
    expect(order).toEqual([
      "start:daemon",
      "start:health",
      "stop:health",
      "stop:daemon",
    ]);
  });
});
