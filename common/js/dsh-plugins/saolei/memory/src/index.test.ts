/**
 * saolei memory adapter tests (specs/074-dsh-grpc-runtime/contracts/
 * team-assembly.md §3.2; specs/076-agent-v2-optimize/contracts/
 * memory-library-layering.md §3): `apply` builds the MemoryService client once
 * from its config over the `ctx.grpc` face, provides the `memoryMount`
 * factory, and `build(scope)` delegates to the memory library's memory-unit
 * factory with the scope passed EXPLICITLY and the runtime's unary policy
 * (no scope derivation, no wiki client, no registration). A missing `grpc`
 * face fails loud.
 *
 * Pattern (style/javascript.md Mock convention): the `grpc` face is a
 * `vi.fn()`-backed stub injected through the real Context; the returned client
 * is a callback-style `vi.fn()` unary double and the policy is a pass-through
 * double — no module interception.
 */

import { Context } from "@deepseek-ai/cordis";
import { describe, expect, it, vi } from "vitest";

import { apply, Config, MEMORY_MOUNT_SERVICE, MEMORY_SERVICE_NAME } from "./index.js";
import type { GrpcClientOptions, MemoryMount } from "./index.js";
import type { MemoryServiceCalls, UnaryPolicy } from "@dominion/common-js-memory";

interface FakeClients {
  readonly memoryClient: MemoryServiceCalls;
  readonly listMemories: ReturnType<typeof vi.fn>;
}

/** Callback-style unary doubles: listMemories answers an empty page. */
function fakeClients(): FakeClients {
  const listMemories = vi.fn(
    (
      _request: unknown,
      _metadata: unknown,
      _options: unknown,
      callback: (
        err: null,
        response: { memories: never[]; nextPageToken: string },
      ) => void,
    ) => {
      callback(null, { memories: [], nextPageToken: "" });
    },
  );
  const memoryClient = {
    createMemory: vi.fn(),
    updateMemory: vi.fn(),
    deleteMemory: vi.fn(),
    listMemories,
  } as unknown as MemoryServiceCalls;
  return { memoryClient, listMemories };
}

/**
 * A `UnaryPolicy` pass-through double (the runtime policy's semantics). The
 * call arguments are typed loosely here so the test needs no grpc-js import:
 * the double's whole job is forwarding the callback.
 */
function policyDouble(): {
  readonly policy: UnaryPolicy;
  readonly spy: ReturnType<typeof vi.fn>;
} {
  type Invoke = (
    client: object,
    metadata: object,
    options: { deadline: Date },
    callback: (err: Error | null, response?: unknown) => void,
  ) => void;
  const spy = vi.fn(
    (client: object, _label: string, invoke: Invoke): Promise<unknown> =>
      new Promise((resolve, reject) => {
        invoke(client, {}, { deadline: new Date() }, (err, res) => {
          if (err !== null) {
            reject(err);
            return;
          }
          resolve(res);
        });
      }),
  );
  return { policy: spy as unknown as UnaryPolicy, spy };
}

interface Harness {
  readonly ctx: Context;
  readonly client: ReturnType<typeof vi.fn>;
  readonly clients: FakeClients;
  readonly policy: UnaryPolicy;
  readonly policySpy: ReturnType<typeof vi.fn>;
  readonly mount: MemoryMount;
}

/** Apply the adapter with a stub grpc face and read back the provided mount. */
function harness(options: { readonly omitGrpc?: boolean } = {}): Harness {
  const clients = fakeClients();
  const client = vi.fn((_opts: GrpcClientOptions) => clients.memoryClient);
  const { policy, spy: policySpy } = policyDouble();
  const ctx = new Context();
  if (options.omitGrpc !== true) {
    ctx.provide("grpc", { client, callUnary: policy });
  }
  apply(ctx, {
    target: "dominion:///game/memory:50051",
    protoPath: "projects/game/game.proto",
  });
  const mount = (ctx as unknown as Record<string, MemoryMount>)[
    MEMORY_MOUNT_SERVICE
  ];
  return { ctx, client, clients, policy, policySpy, mount };
}

describe("saolei-memory adapter", () => {
  it("constructs the MemoryService client only (no wiki client)", () => {
    const { client } = harness();

    expect(client).toHaveBeenCalledTimes(1);
    expect(client).toHaveBeenNthCalledWith(1, {
      target: "dominion:///game/memory:50051",
      protoPath: "projects/game/game.proto",
      serviceName: MEMORY_SERVICE_NAME,
    });
  });

  it("passes the optional label through to the client", () => {
    const clients = fakeClients();
    const client = vi.fn(() => clients.memoryClient);
    const { policy } = policyDouble();
    const ctx = new Context();
    ctx.provide("grpc", { client, callUnary: policy });

    apply(ctx, {
      target: "dominion:///game/memory:50051",
      protoPath: "/abs/game.proto",
      label: "game-memory",
    });

    expect(client).toHaveBeenCalledTimes(1);
    expect(client).toHaveBeenNthCalledWith(1, {
      target: "dominion:///game/memory:50051",
      protoPath: "/abs/game.proto",
      serviceName: MEMORY_SERVICE_NAME,
      label: "game-memory",
    });
  });

  it(
    "provides a memoryMount whose build returns the memory unit for the " +
      "passed scope",
    async () => {
    const { mount, clients, policySpy } = harness();

    const scope = { template: "saolei", preset: "planner-p" };
    const unit = mount.build(scope);

    expect(unit.faces.map((registration) => registration.key)).toEqual([
      "memory",
    ]);
    expect(unit.tools.map((tool) => tool.name)).toEqual(["memory"]);
    expect(unit.sections.map((section) => section.name)).toEqual([
      "memory:snapshot",
    ]);
    expect(unit.ready).toBeTypeOf("function");

    // The scope is used exactly as passed and the runtime's policy is the
    // invocation path: ready() prefetches the scope's collection through the
    // injected memory client and the injected policy.
    await unit.ready();
    expect(clients.listMemories).toHaveBeenCalledTimes(1);
    expect(clients.listMemories.mock.calls[0]?.[0]).toMatchObject({
      parent: "templates/saolei/presets/planner-p",
    });
    expect(policySpy).toHaveBeenCalledWith(
      clients.memoryClient,
      "memory client call",
      expect.any(Function),
    );
  });

  it("requires target and protoPath through the Config schema", () => {
    expect(() => Config({})).toThrow();
    expect(Config({ target: "t", protoPath: "p" })).toMatchObject({
      target: "t",
      protoPath: "p",
    });
  });

  it("fails loud when the grpc face is not composed", () => {
    expect(() => harness({ omitGrpc: true })).toThrow(
      /grpc service is not composed/,
    );
  });
});
