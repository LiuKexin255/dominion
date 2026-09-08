import { describe, it, expect, vi } from "vitest";
import type { Server as GrpcServer, ServerCredentials as GrpcServerCredentials } from "@grpc/grpc-js";
import { createGrpcServerComponent, type GrpcServerComponentOptions } from "./grpc-server.js";

// Structured fake of the grpc-js Server surface used by the adapter (DI
// injection, no real grpc instance, no runtime grpc-js load), mirroring the
// API semantics: bindAsync callback, tryShutdown waits for pending calls,
// forceShutdown is idempotent with tryShutdown and releases its callback.

function fakeGrpcServer(opts: { bindError?: Error } = {}) {
  let tryShutdownCb: ((err?: Error) => void) | undefined;
  const bindAsync = vi.fn(
    (_port: string, _creds: unknown, cb: (err: Error | null, port: number) => void) => {
      if (opts.bindError) cb(opts.bindError, 0);
      else cb(null, 50051);
    },
  );
  const start = vi.fn();
  const tryShutdown = vi.fn((cb: (err?: Error) => void) => {
    tryShutdownCb = cb;
  });
  const forceShutdown = vi.fn(() => {
    tryShutdownCb?.();
  });
  return {
    server: { bindAsync, start, tryShutdown, forceShutdown } as unknown as GrpcServer,
    bindAsync,
    start,
    tryShutdown,
    forceShutdown,
    releaseTryShutdown: (err?: Error) => tryShutdownCb?.(err),
  };
}

function makeOptions(server: GrpcServer): GrpcServerComponentOptions {
  return { server, address: "0.0.0.0:50051", credentials: {} as GrpcServerCredentials };
}

function neverAborted(): AbortSignal {
  return new AbortController().signal;
}

describe("createGrpcServerComponent", () => {
  it("provides no exited promise (no serve-loop exit signal)", () => {
    const fake = fakeGrpcServer();
    const c = createGrpcServerComponent("grpc", makeOptions(fake.server));
    expect("exited" in c).toBe(false);
  });

  it("binds and starts on start", async () => {
    const fake = fakeGrpcServer();
    const c = createGrpcServerComponent("grpc", makeOptions(fake.server));
    await c.start(neverAborted());
    expect(fake.bindAsync).toHaveBeenCalledOnce();
    expect(fake.bindAsync.mock.calls[0][0]).toBe("0.0.0.0:50051");
    expect(fake.start).toHaveBeenCalledOnce();
  });

  it("rejects start when bindAsync fails", async () => {
    const fake = fakeGrpcServer({ bindError: new Error("address in use") });
    const c = createGrpcServerComponent("grpc", makeOptions(fake.server));
    await expect(c.start(neverAborted())).rejects.toThrow("address in use");
  });

  it("stops gracefully via tryShutdown without forcing", async () => {
    const fake = fakeGrpcServer();
    const c = createGrpcServerComponent("grpc", makeOptions(fake.server));
    await c.start(neverAborted());

    const stopPromise = c.stop(neverAborted());
    expect(fake.tryShutdown).toHaveBeenCalledOnce();
    // Simulate pending calls completing.
    fake.releaseTryShutdown();
    await expect(stopPromise).resolves.toBeUndefined();
    expect(fake.forceShutdown).not.toHaveBeenCalled();
  });

  it("falls back to forceShutdown when the budget signal aborts", async () => {
    const fake = fakeGrpcServer();
    const c = createGrpcServerComponent("grpc", makeOptions(fake.server));
    await c.start(neverAborted());

    // tryShutdown stays pending (pending calls never finish); the abort must
    // trigger forceShutdown, which releases the outstanding callback.
    const stopPromise = c.stop(AbortSignal.timeout(30));
    await expect(stopPromise).resolves.toBeUndefined();
    expect(fake.tryShutdown).toHaveBeenCalledOnce();
    expect(fake.forceShutdown).toHaveBeenCalledOnce();
  });
});
