/**
 * saolei memory adapter tests (specs/074-dsh-grpc-runtime/contracts/
 * team-assembly.md §3.2): `apply` builds the two gRPC clients once from its
 * config over the `ctx.grpc` face, provides the `memoryMount` factory, and
 * `build(scope)` delegates to the memory library's atomic unit factory with
 * the scope passed EXPLICITLY (no scope derivation, no registration). A
 * missing `grpc` face fails loud.
 *
 * Pattern (style/javascript.md Mock convention): the `grpc` face is a
 * `vi.fn()`-backed stub injected through the real Context; the returned
 * clients are callback-style `vi.fn()` unary doubles — no module
 * interception.
 */

import { Context } from "@deepseek-ai/cordis";
import { describe, expect, it, vi } from "vitest";

import {
  apply,
  Config,
  MEMORY_MOUNT_SERVICE,
  MEMORY_SERVICE_NAME,
  WIKI_SERVICE_NAME,
} from "./index.js";
import type { GrpcClientOptions, MemoryMount } from "./index.js";
import type {
  MemoryServiceCalls,
  WikiServiceCalls,
} from "@dominion/common-js-memory";

interface FakeClients {
  readonly memoryClient: MemoryServiceCalls;
  readonly wikiClient: WikiServiceCalls;
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
  const wikiClient = {
    searchWikis: vi.fn(),
    getWiki: vi.fn(),
    createWiki: vi.fn(),
    updateWiki: vi.fn(),
    deleteWiki: vi.fn(),
    listWikis: vi.fn(),
  } as unknown as WikiServiceCalls;
  return { memoryClient, wikiClient, listMemories };
}

interface Harness {
  readonly ctx: Context;
  readonly client: ReturnType<typeof vi.fn>;
  readonly clients: FakeClients;
  readonly mount: MemoryMount;
}

/** Apply the adapter with a stub grpc face and read back the provided mount. */
function harness(options: { readonly omitGrpc?: boolean } = {}): Harness {
  const clients = fakeClients();
  const client = vi.fn((opts: GrpcClientOptions) =>
    opts.serviceName === MEMORY_SERVICE_NAME
      ? clients.memoryClient
      : clients.wikiClient,
  );
  const ctx = new Context();
  if (options.omitGrpc !== true) {
    ctx.provide("grpc", { client });
  }
  apply(ctx, {
    target: "dominion:///game/memory:50051",
    protoPath: "projects/game/game.proto",
  });
  const mount = (ctx as unknown as Record<string, MemoryMount>)[
    MEMORY_MOUNT_SERVICE
  ];
  return { ctx, client, clients, mount };
}

describe("saolei-memory adapter", () => {
  it(
    "constructs both service clients from the config over the shared grpc " +
      "face",
    () => {
    const { client } = harness();

    expect(client).toHaveBeenCalledTimes(2);
    expect(client).toHaveBeenNthCalledWith(1, {
      target: "dominion:///game/memory:50051",
      protoPath: "projects/game/game.proto",
      serviceName: MEMORY_SERVICE_NAME,
    });
    expect(client).toHaveBeenNthCalledWith(2, {
      target: "dominion:///game/memory:50051",
      protoPath: "projects/game/game.proto",
      serviceName: WIKI_SERVICE_NAME,
    });
  });

  it("passes the optional label through to both clients", () => {
    const clients = fakeClients();
    const client = vi.fn(() => clients.memoryClient);
    const ctx = new Context();
    ctx.provide("grpc", { client });

    apply(ctx, {
      target: "dominion:///game/memory:50051",
      protoPath: "/abs/game.proto",
      label: "game-memory",
    });

    expect(client).toHaveBeenNthCalledWith(1, {
      target: "dominion:///game/memory:50051",
      protoPath: "/abs/game.proto",
      serviceName: MEMORY_SERVICE_NAME,
      label: "game-memory",
    });
    expect(client).toHaveBeenNthCalledWith(2, {
      target: "dominion:///game/memory:50051",
      protoPath: "/abs/game.proto",
      serviceName: WIKI_SERVICE_NAME,
      label: "game-memory",
    });
  });

  it(
    "provides a memoryMount whose build returns the atomic unit for the " +
      "passed scope",
    async () => {
    const { mount, clients } = harness();

    const scope = { template: "saolei", preset: "planner-p" };
    const unit = mount.build(scope);

    expect(unit.faces.map((registration) => registration.key)).toEqual([
      "memory",
    ]);
    expect(unit.tools).toHaveLength(4);
    expect(unit.sections.map((section) => section.name)).toEqual([
      "memory:guidance",
      "wiki:guidance",
      "memory:snapshot",
    ]);
    expect(unit.ready).toBeTypeOf("function");

    // The scope is used exactly as passed: ready() prefetches the scope's
    // collection through the injected memory client.
    await unit.ready();
    expect(clients.listMemories).toHaveBeenCalledTimes(1);
    expect(clients.listMemories.mock.calls[0]?.[0]).toMatchObject({
      parent: "templates/saolei/presets/planner-p",
    });
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
