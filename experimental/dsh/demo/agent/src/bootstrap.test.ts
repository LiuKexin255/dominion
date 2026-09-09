import { describe, expect, it, vi } from "vitest";
import { runAgent } from "./bootstrap.js";
import type { DshContext } from "./dsh.js";
import type { AgentSessions } from "./session.js";
import type { BuiltChatServer } from "./server.js";

/**
 * Lifecycle unit tests for the orchestrator wiring in bootstrap.ts
 * (specs/053-js-bootstrap-migration/contracts/bootstrap-js-api.md §3.1/§8):
 * component start order (composition before the grpc server), the health
 * endpoint's FIFO position (started after every component, stopped before
 * any), the reverse stop order (grpc drain before session disposal), and
 * the fail-loud semantics (a dsh boot failure or a grpc bind failure rolls
 * back the started peers and rejects the run).
 *
 * All collaborators are `vi.fn()` doubles injected through the AgentRunDeps
 * seam; the built-in health endpoint is replaced through the orchestrator's
 * @internal healthServerFactory test seam (production never passes it) — no
 * module interception (style/javascript.md Mock convention).
 */

type Listener = (...args: never[]) => void;

/** Shared call-order log every fake appends to. */
function createHarness(bindError?: Error) {
  const events: string[] = [];

  const boot = vi.fn(async (): Promise<DshContext> => {
    events.push("composition.start");
    return { marker: "ctx" } as unknown as DshContext;
  });

  const grpcServer = {
    bindAsync: vi.fn(
      (_address: string, _credentials: unknown, callback: (err: Error | null) => void) => {
        events.push("grpc.bind");
        callback(bindError ?? null);
      },
    ),
    start: vi.fn(),
    tryShutdown: vi.fn((callback: (err: Error | null) => void) => {
      events.push("grpc.tryShutdown");
      callback(null);
    }),
    forceShutdown: vi.fn(),
  };

  const sessionsShutdown = vi.fn(async () => {
    events.push("sessions.shutdown");
  });

  const buildChatServer = vi.fn(
    async (): Promise<BuiltChatServer> => {
      events.push("server.build");
      return {
        server: grpcServer as unknown as BuiltChatServer["server"],
        credentials: {} as BuiltChatServer["credentials"],
        sessions: { shutdown: sessionsShutdown } as unknown as AgentSessions,
      };
    },
  );

  const health = {
    start: vi.fn(async () => {
      events.push("health.start");
    }),
    stop: vi.fn(async () => {
      events.push("health.stop");
    }),
  };

  return {
    events,
    deps: {
      boot,
      buildChatServer,
      // The healthServerFactory is the orchestrator's @internal test seam;
      // only tests pass it, so no test run ever binds the real :38080.
      bootstrapOptions: { healthServerFactory: () => health },
    },
    health,
    grpcServer,
    sessionsShutdown,
  };
}

/** Drain the microtask queue so the async start chain settles. */
async function flush(times = 20): Promise<void> {
  for (let i = 0; i < times; i++) {
    await Promise.resolve();
  }
}

describe("runAgent lifecycle", () => {
  it("starts health last, stops health first, and disposes sessions after the grpc drain", async () => {
    const harness = createHarness();
    const controller = new AbortController();
    const run = runAgent({ ...harness.deps, runOptions: { signal: controller.signal } });

    // Let the start chain settle: composition → server → grpc → health,
    // after which the orchestrator parks in the exit wait.
    await flush();
    controller.abort();

    await expect(run).resolves.toBeUndefined();
    expect(harness.events).toEqual([
      "composition.start",
      "server.build",
      "grpc.bind",
      "health.start",
      "health.stop",
      "grpc.tryShutdown",
      "sessions.shutdown",
    ]);
    expect(harness.grpcServer.start).toHaveBeenCalledTimes(1);
    expect(harness.grpcServer.forceShutdown).not.toHaveBeenCalled();
  });

  it("fails loud on a grpc bind failure and rolls the composition back", async () => {
    const harness = createHarness(new Error("bind failed: EADDRINUSE"));
    const controller = new AbortController();
    const run = runAgent({ ...harness.deps, runOptions: { signal: controller.signal } });

    await flush();
    await expect(run).rejects.toThrow(/grpc-chat/);

    // The health endpoint never starts when a component start fails, and
    // the rollback disposes the already-created session registry.
    expect(harness.events).toEqual([
      "composition.start",
      "server.build",
      "grpc.bind",
      "sessions.shutdown",
    ]);
    expect(harness.health.start).not.toHaveBeenCalled();
  });

  it("fails loud on a dsh boot failure before any other component starts", async () => {
    const harness = createHarness();
    harness.deps.boot = vi.fn(async () => {
      harness.events.push("composition.start");
      throw new Error("cordis.yml row 3: peer dependency missing");
    });
    const controller = new AbortController();
    const run = runAgent({ ...harness.deps, runOptions: { signal: controller.signal } });

    await flush();
    await expect(run).rejects.toThrow("dsh-composition");

    // Nothing else started: no server build, no bind, no health, and the
    // session registry never existed to dispose.
    expect(harness.events).toEqual(["composition.start"]);
    expect(harness.health.start).not.toHaveBeenCalled();
    expect(harness.sessionsShutdown).not.toHaveBeenCalled();
  });
});
