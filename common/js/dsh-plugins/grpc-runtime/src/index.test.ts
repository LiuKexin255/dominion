/**
 * Plugin entry tests (specs/074-dsh-grpc-runtime/tasks.md T004).
 *
 * `apply` receives injected `registerResolver`/`createRuntime` doubles (DI;
 * no `vi.mock` — style/javascript.md §测试) so the registration order,
 * `ctx.grpc` provision and composition teardown are asserted directly. The
 * OTel loader-hook ordering contract
 * (specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md
 * §2) is upheld structurally: this module performs no channel creation at
 * load time, and `apply` registers the resolver before the runtime (the only
 * channel owner) is created.
 */

import { describe, expect, it, vi } from "vitest";

import { apply } from "./index.js";
import type { GrpcRuntime } from "./client.js";

type Ctx = Parameters<typeof apply>[0];

function makeCtx(): {
  ctx: Ctx;
  provide: ReturnType<typeof vi.fn>;
  effect: ReturnType<typeof vi.fn>;
} {
  const provide = vi.fn();
  const effect = vi.fn(
    (_body: () => () => void, _label?: string) => ({}),
  );
  return { ctx: { provide, effect } as unknown as Ctx, provide, effect };
}

function fakeRuntime(): GrpcRuntime {
  return {
    client: vi.fn(() => ({})),
    callUnary: vi.fn(async () => undefined),
  } as unknown as GrpcRuntime;
}

describe("grpc-runtime plugin", () => {
  it("registers the resolver first, then provides ctx.grpc", () => {
    const order: string[] = [];
    const registerResolver = vi.fn(() => {
      order.push("resolver");
    });
    const runtime = fakeRuntime();
    const close = vi.fn();
    const createRuntime = vi.fn(() => {
      order.push("runtime");
      return { runtime, close };
    });
    const { ctx, provide, effect } = makeCtx();

    apply(ctx, { registerResolver, createRuntime });

    expect(registerResolver).toHaveBeenCalledTimes(1);
    expect(createRuntime).toHaveBeenCalledTimes(1);
    // Resolver registration precedes runtime creation (the only point that
    // can create a channel) — the ordering contract from contracts §4.
    expect(order).toEqual(["resolver", "runtime"]);
    expect(provide).toHaveBeenCalledWith("grpc", runtime);
    expect(effect).toHaveBeenCalledTimes(1);
  });

  it("registers the channel teardown as a composition effect", () => {
    const close = vi.fn();
    const { ctx, effect } = makeCtx();

    apply(ctx, {
      registerResolver: vi.fn(),
      createRuntime: () => ({ runtime: fakeRuntime(), close }),
    });

    expect(effect).toHaveBeenCalledTimes(1);
    const body = effect.mock.calls[0][0] as () => () => void;
    const dispose = body();
    dispose();
    expect(close).toHaveBeenCalledTimes(1);
    expect(effect.mock.calls[0][1]).toBe("grpc-runtime.channels");
  });

  it("applies with production defaults without creating a channel", () => {
    const { ctx, provide, effect } = makeCtx();

    apply(ctx);

    const grpc = provide.mock.calls[0]?.[1] as GrpcRuntime;
    expect(typeof grpc.client).toBe("function");
    expect(typeof grpc.callUnary).toBe("function");
    expect(effect).toHaveBeenCalledTimes(1);
    // The real resolver registration is idempotent, so this default path is
    // safe to exercise.
    expect(provide.mock.calls[0]?.[0]).toBe("grpc");
  });

  it("falls back to production defaults for non-function deps slots", () => {
    // This row MUST carry no config; an accidental config object must not
    // produce a confusing error (contracts/grpc-runtime.md §1).
    const { ctx, provide, effect } = makeCtx();

    apply(ctx, {
      registerResolver: {} as unknown as () => void,
      createRuntime: null as unknown as () => never,
    });

    const grpc = provide.mock.calls[0]?.[1] as GrpcRuntime;
    expect(typeof grpc.client).toBe("function");
    expect(typeof grpc.callUnary).toBe("function");
    expect(effect).toHaveBeenCalledTimes(1);
  });
});
