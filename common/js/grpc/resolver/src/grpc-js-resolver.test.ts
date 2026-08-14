import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import type { GrpcUri } from "@grpc/grpc-js/build/src/uri-parser";
import type { ResolverListener } from "@grpc/grpc-js/build/src/resolver";
import type { Endpoint } from "@grpc/grpc-js/build/src/subchannel-address";
import { Status } from "@grpc/grpc-js/build/src/constants";
import {
  registerDominionResolver,
  DominionResolver,
  DominionStatefulResolver,
} from "./grpc-js-resolver";
import type { Scheduler, ResolverConfig } from "@dominion/common-js-resolver";

const TEST_ENV = {
  DOMINION_ENVIRONMENT: "dev.alpha",
  SERVICE_APP: "myapp",
};

function fakeFetchReturning(body: unknown): ReturnType<typeof vi.fn> {
  return vi.fn(async () => {
    const json = JSON.stringify(body);
    return {
      ok: true,
      status: 200,
      json: async () => JSON.parse(json),
      text: async () => json,
    } as Response;
  });
}

interface SpyScheduler extends Scheduler {
  setInterval: ReturnType<typeof vi.fn>;
  clearInterval: ReturnType<typeof vi.fn>;
}

function spyScheduler(): SpyScheduler {
  return {
    setInterval: vi.fn(() => 42),
    clearInterval: vi.fn(),
  };
}

function mockListener(): ReturnType<typeof vi.fn> & ResolverListener {
  return vi.fn(() => true) as ReturnType<typeof vi.fn> & ResolverListener;
}

function dominionTarget(path: string): GrpcUri {
  return { scheme: "dominion", authority: "", path };
}

function statefulTarget(path: string): GrpcUri {
  return { scheme: "dominion-stateful", authority: "", path };
}

function sortedEndpoints(eps: Endpoint[]): Endpoint[] {
  return [...eps].sort((a, b) => {
    const ha = a.addresses[0] as { host: string; port: number };
    const hb = b.addresses[0] as { host: string; port: number };
    return ha.host.localeCompare(hb.host);
  });
}

describe("registerDominionResolver", () => {
  it("registers both schemes on first call", () => {
    expect(() => registerDominionResolver()).not.toThrow();
  });

  it("is idempotent: calling twice does not throw", () => {
    registerDominionResolver();
    expect(() => registerDominionResolver()).not.toThrow();
  });
});

describe("DominionResolver", () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval", "setImmediate", "clearImmediate"] });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("successful resolution: listener receives StatusOr with ok=true and correct endpoints", async () => {
    const fetch = fakeFetchReturning({
      endpoints: ["10.0.0.1:50051", "10.0.0.2:50051"],
      ports: {},
      isStateful: false,
      statefulInstances: [],
    });
    const listener = mockListener();
    const target = dominionTarget("myapp/myservice:50051");

    new DominionResolver(target, listener, {}, {
      env: TEST_ENV,
      fetch,
      scheduler: spyScheduler(),
    });
    await vi.runAllTimersAsync();

    expect(listener).toHaveBeenCalledOnce();
    const call = listener.mock.calls[0];
    expect(call[0].ok).toBe(true);
    const endpoints = sortedEndpoints(call[0].value as Endpoint[]);
    expect(endpoints).toEqual([
      { addresses: [{ host: "10.0.0.1", port: 50051 }] },
      { addresses: [{ host: "10.0.0.2", port: 50051 }] },
    ]);
    expect(call[1]).toEqual({});
    expect(call[2]).toBeNull();
    expect(call[3]).toBe("");
  });

  it("async publication: listener not called synchronously from constructor or updateResolution", () => {
    const fetch = fakeFetchReturning({
      endpoints: ["10.0.0.1:50051"],
      ports: {},
      isStateful: false,
      statefulInstances: [],
    });
    const listener = mockListener();
    const target = dominionTarget("myapp/myservice:50051");

    const resolver = new DominionResolver(target, listener, {}, { env: TEST_ENV, fetch });
    expect(listener).not.toHaveBeenCalled();

    resolver.updateResolution();
    expect(listener).not.toHaveBeenCalled();
  });

  it("unchanged refresh: same endpoints do not trigger duplicate listener call", async () => {
    const fetch = fakeFetchReturning({
      endpoints: ["10.0.0.1:50051"],
      ports: {},
      isStateful: false,
      statefulInstances: [],
    });
    const listener = mockListener();
    const target = dominionTarget("myapp/myservice:50051");

    new DominionResolver(target, listener, {}, {
      env: TEST_ENV,
      fetch,
      scheduler: spyScheduler(),
    });
    await vi.runAllTimersAsync();
    expect(listener).toHaveBeenCalledTimes(1);

    listener.mockClear();

    // Trigger refresh via advancing interval timer
    await vi.advanceTimersByTimeAsync(30_000);
    expect(listener).not.toHaveBeenCalled();
  });

  it("refresh failure with previous state (LKG): listener called with ok=false, last endpoints retained", async () => {
    const listener = mockListener();
    const target = dominionTarget("myapp/myservice:50051");

    // A fetch that succeeds once, then fails on refresh. The first refresh
    // (initial resolution) succeeds; the second (triggered via
    // updateResolution) fails, exercising the LKG-with-error path.
    let callCount = 0;
    const togglingFetch = vi.fn(async () => {
      callCount++;
      if (callCount === 1) {
        const json = JSON.stringify({
          endpoints: ["10.0.0.1:50051"],
          ports: {},
          isStateful: false,
          statefulInstances: [],
        });
        return {
          ok: true,
          status: 200,
          json: async () => JSON.parse(json),
          text: async () => json,
        } as Response;
      }
      throw new Error("network error");
    });

    // Inject a spy scheduler so the repeating setInterval does not loop
    // vi.runAllTimersAsync; refresh is driven explicitly via updateResolution
    // (which schedules a one-shot setImmediate that runAllTimersAsync drains).
    const lkgResolver = new DominionResolver(
      target,
      listener,
      {},
      { env: TEST_ENV, fetch: togglingFetch, scheduler: spyScheduler() },
    );
    await vi.runAllTimersAsync();
    expect(listener).toHaveBeenCalledTimes(1);
    expect(listener.mock.calls[0][0].ok).toBe(true);
    listener.mockClear();

    // Trigger a refresh that fails.
    lkgResolver.updateResolution();
    await vi.runAllTimersAsync();
    expect(listener).toHaveBeenCalledTimes(1);
    const lkgCall = listener.mock.calls[0];
    expect(lkgCall[0].ok).toBe(false);
    expect(lkgCall[0].error.code).toBe(Status.UNAVAILABLE);
    // deploy-client wraps network errors as DeployServiceError with a
    // "deploy service request failed: " prefix (deploy-client.ts:61).
    expect(lkgCall[0].error.details).toBe(
      "deploy service request failed: network error",
    );
  });

  it("refresh failure followed by success with same endpoints re-emits ok=true", async () => {
    const listener = mockListener();
    const target = dominionTarget("myapp/myservice:50051");
    const endpointsBody = {
      endpoints: ["10.0.0.1:50051"],
      ports: {},
      isStateful: false,
      statefulInstances: [],
    };

    // Fetch sequence: success -> failure -> success (same endpoints).
    let callCount = 0;
    const togglingFetch = vi.fn(async () => {
      callCount++;
      if (callCount === 1 || callCount === 3) {
        return {
          ok: true,
          status: 200,
          json: async () => JSON.parse(JSON.stringify(endpointsBody)),
          text: async () => JSON.stringify(endpointsBody),
        } as Response;
      }
      throw new Error("network error");
    });

    const resolver = new DominionResolver(target, listener, {}, {
      env: TEST_ENV,
      fetch: togglingFetch,
      scheduler: spyScheduler(),
    });
    await vi.runAllTimersAsync();
    expect(listener).toHaveBeenCalledTimes(1);
    expect(listener.mock.calls[0][0].ok).toBe(true);
    listener.mockClear();

    resolver.updateResolution();
    await vi.runAllTimersAsync();
    expect(listener).toHaveBeenCalledTimes(1);
    expect(listener.mock.calls[0][0].ok).toBe(false);
    listener.mockClear();

    // The channel may have entered a degraded state from the error emit;
    // the next success MUST re-emit even though endpoints are unchanged.
    resolver.updateResolution();
    await vi.runAllTimersAsync();
    expect(listener).toHaveBeenCalledTimes(1);
    expect(listener.mock.calls[0][0].ok).toBe(true);
  });

  it("passes an abort-signal with requestTimeoutMs to the deploy fetch", async () => {
    const fetch = fakeFetchReturning({
      endpoints: ["10.0.0.1:50051"],
      ports: {},
      isStateful: false,
      statefulInstances: [],
    });
    const listener = mockListener();
    const target = dominionTarget("myapp/myservice:50051");

    new DominionResolver(target, listener, {}, {
      env: TEST_ENV,
      fetch,
      scheduler: spyScheduler(),
      requestTimeoutMs: 7_000,
    });
    await vi.runAllTimersAsync();

    expect(fetch).toHaveBeenCalledOnce();
    const init = fetch.mock.calls[0][1] as RequestInit;
    expect(init.signal).toBeInstanceOf(AbortSignal);
    expect(init.signal?.aborted).toBe(false);
  });

  it("initial failure: listener called with ok=false, no endpoint data", async () => {
    const failFetch = vi.fn(async () => {
      throw new Error("initial failure");
    });
    const listener = mockListener();
    const target = dominionTarget("myapp/myservice:50051");

    new DominionResolver(target, listener, {}, {
      env: TEST_ENV,
      fetch: failFetch,
      scheduler: spyScheduler(),
    });
    await vi.runAllTimersAsync();

    expect(listener).toHaveBeenCalledOnce();
    const call = listener.mock.calls[0];
    expect(call[0].ok).toBe(false);
    expect(call[0].error.code).toBe(Status.UNAVAILABLE);
    // deploy-client wraps network errors as DeployServiceError with a
    // "deploy service request failed: " prefix (deploy-client.ts:61).
    expect(call[0].error.details).toBe(
      "deploy service request failed: initial failure",
    );
  });

  it("timer cleanup on destroy: no further listener calls after destroy()", async () => {
    let fetchCount = 0;
    const fetch = vi.fn(async () => {
      fetchCount++;
      const json = JSON.stringify({
        endpoints: ["10.0.0.1:50051"],
        ports: {},
        isStateful: false,
        statefulInstances: [],
      });
      return {
        ok: true,
        status: 200,
        json: async () => JSON.parse(json),
        text: async () => json,
      } as Response;
    });
    const listener = mockListener();
    const target = dominionTarget("myapp/myservice:50051");

    const resolver = new DominionResolver(target, listener, {}, {
      env: TEST_ENV,
      fetch,
      scheduler: spyScheduler(),
    });
    await vi.runAllTimersAsync();
    expect(listener).toHaveBeenCalledTimes(1);
    listener.mockClear();

    resolver.destroy();

    // Advance past multiple intervals
    await vi.advanceTimersByTimeAsync(60_000);
    expect(listener).not.toHaveBeenCalled();
  });

  it("closed state: updateResolution() after destroy() is a no-op", async () => {
    const fetch = fakeFetchReturning({
      endpoints: ["10.0.0.1:50051"],
      ports: {},
      isStateful: false,
      statefulInstances: [],
    });
    const listener = mockListener();
    const target = dominionTarget("myapp/myservice:50051");

    const resolver = new DominionResolver(target, listener, {}, {
      env: TEST_ENV,
      fetch,
      scheduler: spyScheduler(),
    });
    await vi.runAllTimersAsync();
    expect(listener).toHaveBeenCalledTimes(1);
    listener.mockClear();

    resolver.destroy();
    resolver.updateResolution();
    await vi.runAllTimersAsync();

    expect(listener).not.toHaveBeenCalled();
  });

  it("custom scheduler: uses injected scheduler for refresh timer", () => {
    const fetch = fakeFetchReturning({
      endpoints: ["10.0.0.1:50051"],
      ports: {},
      isStateful: false,
      statefulInstances: [],
    });
    const sched = spyScheduler();
    const listener = mockListener();
    const target = dominionTarget("myapp/myservice:50051");

    new DominionResolver(target, listener, {}, {
      env: TEST_ENV,
      fetch,
      scheduler: sched,
      refreshIntervalMs: 10_000,
    });

    expect(sched.setInterval).toHaveBeenCalledOnce();
    const [cb, ms] = sched.setInterval.mock.calls[0];
    expect(typeof cb).toBe("function");
    expect(ms).toBe(10_000);
  });

  it("empty endpoints with prior valid state retain endpoints (no listener call)", async () => {
    // Deploy incident 2026-08-09 (prompt rollout): the deploy service
    // returned 200 with zero ready endpoints during the rollout gap.
    // Publishing that empty list to grpc-js makes round_robin destroy all
    // subchannels and enter IDLE with no self-recovery path; the resolver
    // MUST retain the prior valid endpoints instead.
    let callCount = 0;
    const fetch = vi.fn(async () => {
      callCount++;
      const endpoints = callCount === 1 ? ["10.0.0.1:50051"] : [];
      const json = JSON.stringify({
        endpoints,
        ports: {},
        isStateful: false,
        statefulInstances: [],
      });
      return {
        ok: true,
        status: 200,
        json: async () => JSON.parse(json),
        text: async () => json,
      } as Response;
    });
    const listener = mockListener();
    const target = dominionTarget("myapp/myservice:50051");

    const resolver = new DominionResolver(target, listener, {}, {
      env: TEST_ENV,
      fetch,
      scheduler: spyScheduler(),
    });
    await vi.runAllTimersAsync();
    expect(listener).toHaveBeenCalledTimes(1);
    expect(listener.mock.calls[0][0].ok).toBe(true);
    listener.mockClear();

    resolver.updateResolution();
    await vi.runAllTimersAsync();
    expect(listener).not.toHaveBeenCalled();
  });

  it("empty endpoints on initial resolution emits UNAVAILABLE", async () => {
    const fetch = fakeFetchReturning({
      endpoints: [],
      ports: {},
      isStateful: false,
      statefulInstances: [],
    });
    const listener = mockListener();
    const target = dominionTarget("myapp/myservice:50051");

    new DominionResolver(target, listener, {}, {
      env: TEST_ENV,
      fetch,
      scheduler: spyScheduler(),
    });
    await vi.runAllTimersAsync();

    expect(listener).toHaveBeenCalledOnce();
    const call = listener.mock.calls[0];
    expect(call[0].ok).toBe(false);
    expect(call[0].error.code).toBe(Status.UNAVAILABLE);
    expect(call[0].error.details).toBe(
      "no endpoints resolved for myapp/myservice:50051",
    );
  });

  it("empty endpoints followed by non-empty endpoints recover on the next refresh", async () => {
    let callCount = 0;
    const fetch = vi.fn(async () => {
      callCount++;
      const endpoints =
        callCount === 1
          ? ["10.0.0.1:50051"]
          : callCount === 2
            ? []
            : ["10.0.0.2:50051"];
      const json = JSON.stringify({
        endpoints,
        ports: {},
        isStateful: false,
        statefulInstances: [],
      });
      return {
        ok: true,
        status: 200,
        json: async () => JSON.parse(json),
        text: async () => json,
      } as Response;
    });
    const listener = mockListener();
    const target = dominionTarget("myapp/myservice:50051");

    const resolver = new DominionResolver(target, listener, {}, {
      env: TEST_ENV,
      fetch,
      scheduler: spyScheduler(),
    });
    await vi.runAllTimersAsync();
    expect(listener).toHaveBeenCalledTimes(1);
    expect(listener.mock.calls[0][0].ok).toBe(true);
    listener.mockClear();

    resolver.updateResolution();
    await vi.runAllTimersAsync();
    expect(listener).not.toHaveBeenCalled();

    resolver.updateResolution();
    await vi.runAllTimersAsync();
    expect(listener).toHaveBeenCalledTimes(1);
    const call = listener.mock.calls[0];
    expect(call[0].ok).toBe(true);
    const endpoints = sortedEndpoints(call[0].value as Endpoint[]);
    expect(endpoints).toEqual([
      { addresses: [{ host: "10.0.0.2", port: 50051 }] },
    ]);
  });

  it("repeated empty endpoints with prior state never emit", async () => {
    let callCount = 0;
    const fetch = vi.fn(async () => {
      callCount++;
      const endpoints = callCount === 1 ? ["10.0.0.1:50051"] : [];
      const json = JSON.stringify({
        endpoints,
        ports: {},
        isStateful: false,
        statefulInstances: [],
      });
      return {
        ok: true,
        status: 200,
        json: async () => JSON.parse(json),
        text: async () => json,
      } as Response;
    });
    const listener = mockListener();
    const target = dominionTarget("myapp/myservice:50051");

    const resolver = new DominionResolver(target, listener, {}, {
      env: TEST_ENV,
      fetch,
      scheduler: spyScheduler(),
    });
    await vi.runAllTimersAsync();
    expect(listener).toHaveBeenCalledTimes(1);
    listener.mockClear();

    resolver.updateResolution();
    await vi.runAllTimersAsync();
    resolver.updateResolution();
    await vi.runAllTimersAsync();
    resolver.updateResolution();
    await vi.runAllTimersAsync();

    expect(listener).not.toHaveBeenCalled();
  });
});

describe("DominionStatefulResolver", () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval", "setImmediate", "clearImmediate"] });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("stateful scheme: resolves instance endpoints from dominion-stateful:///app/svc:port?instance=N", async () => {
    // Named-port targets were REMOVED (grpc-js resolver cannot identify the
    // name part; parseTarget rejects non-numeric ports — target.ts:28-29), so
    // the target uses a numeric port and the fixture endpoints are published
    // on that port directly. A spy scheduler is injected so the resolver's
    // repeating setInterval does not loop vi.runAllTimersAsync.
    const fetch = fakeFetchReturning({
      endpoints: [],
      ports: {},
      isStateful: true,
      statefulInstances: [
        {
          index: 1,
          endpoints: ["10.0.0.1:50051", "10.0.0.2:50051"],
          hostname: "instance-1",
        },
      ],
    });
    const listener = mockListener();
    const target = statefulTarget("myapp/myservice:50051?instance=1");

    new DominionStatefulResolver(target, listener, {}, {
      env: TEST_ENV,
      fetch,
      scheduler: spyScheduler(),
    });
    await vi.runAllTimersAsync();

    expect(listener).toHaveBeenCalledOnce();
    const call = listener.mock.calls[0];
    expect(call[0].ok).toBe(true);
    const endpoints = sortedEndpoints(call[0].value as Endpoint[]);
    expect(endpoints).toEqual([
      { addresses: [{ host: "10.0.0.1", port: 50051 }] },
      { addresses: [{ host: "10.0.0.2", port: 50051 }] },
    ]);
  });

  it("stateful scheme: re-emits ok=true after a failed refresh with same endpoints", async () => {
    const listener = mockListener();
    const target = statefulTarget("myapp/myservice:50051?instance=1");
    const endpointsBody = {
      endpoints: [],
      ports: {},
      isStateful: true,
      statefulInstances: [
        {
          index: 1,
          endpoints: ["10.0.0.1:50051"],
          hostname: "instance-1",
        },
      ],
    };

    let callCount = 0;
    const togglingFetch = vi.fn(async () => {
      callCount++;
      if (callCount === 1 || callCount === 3) {
        return {
          ok: true,
          status: 200,
          json: async () => JSON.parse(JSON.stringify(endpointsBody)),
          text: async () => JSON.stringify(endpointsBody),
        } as Response;
      }
      throw new Error("network error");
    });

    const resolver = new DominionStatefulResolver(target, listener, {}, {
      env: TEST_ENV,
      fetch: togglingFetch,
      scheduler: spyScheduler(),
    });
    await vi.runAllTimersAsync();
    expect(listener).toHaveBeenCalledTimes(1);
    expect(listener.mock.calls[0][0].ok).toBe(true);
    listener.mockClear();

    resolver.updateResolution();
    await vi.runAllTimersAsync();
    expect(listener).toHaveBeenCalledTimes(1);
    expect(listener.mock.calls[0][0].ok).toBe(false);
    listener.mockClear();

    resolver.updateResolution();
    await vi.runAllTimersAsync();
    expect(listener).toHaveBeenCalledTimes(1);
    expect(listener.mock.calls[0][0].ok).toBe(true);
  });
});
