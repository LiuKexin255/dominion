/**
 * Service-client construction tests (specs/074-dsh-grpc-runtime/tasks.md T004).
 *
 * The proto loader, the package-definition loader and the channel pool are
 * injected as `vi.fn()` doubles (DI; no `vi.mock` — style/javascript.md §测试),
 * so the tests exercise canonical path resolution, the package-definition
 * cache, fully-qualified service-name resolution and the shared-channel
 * wiring without touching the filesystem or a live channel.
 */

import { beforeEach, describe, expect, it, vi } from "vitest";
import * as path from "node:path";

import * as grpc from "@grpc/grpc-js";
import * as protoLoader from "@grpc/proto-loader";

import { createGrpcRuntime, resolveProtoPath } from "./client.js";
import type { ChannelPool } from "./channel.js";

// Same derivation as client.ts (both live in this package's src/).
const SERVICE_ROOT = path.resolve(import.meta.dirname, "..", "..", "..", "..");

const TARGET = "dominion:///game/memory:50051";

interface Construction {
  address: string;
  options: unknown;
}

/** A constructable stand-in for a proto-loader service client. */
function fakeServiceClass(constructions: Construction[]) {
  return function FakeService(
    address: string,
    _credentials: unknown,
    options?: unknown,
  ): void {
    constructions.push({ address, options });
  };
}

interface Harness {
  handle: ReturnType<typeof createGrpcRuntime>;
  Service: ReturnType<typeof fakeServiceClass>;
  channel: grpc.Channel;
  get: ReturnType<typeof vi.fn>;
  closeAll: ReturnType<typeof vi.fn>;
}

/**
 * Build a runtime whose proto loader returns `proto`, backed by a fake channel
 * pool. `loadSync` is shared across the harness so cache assertions are stable.
 */
function harness(
  loadSync: ReturnType<typeof vi.fn>,
  proto: grpc.GrpcObject,
): Harness {
  const constructions: Construction[] = [];
  const Service = fakeServiceClass(constructions);
  const channel = { marker: "channel" } as unknown as grpc.Channel;
  const get = vi.fn(() => channel);
  const closeAll = vi.fn();
  const pool = { get, closeAll, size: 0 } as unknown as ChannelPool;
  const loadPackageDefinition = vi.fn(
    (_definition: protoLoader.PackageDefinition) => proto,
  );
  const handle = createGrpcRuntime({ loadSync, loadPackageDefinition, pool });

  return { handle, Service, channel, get, closeAll };
}

function serviceProto(constructions: Construction[]): grpc.GrpcObject {
  return {
    projects: { game: { MemoryService: fakeServiceClass(constructions) } },
  } as unknown as grpc.GrpcObject;
}

describe("resolveProtoPath", () => {
  it("passes absolute paths through (escape hatch)", () => {
    expect(resolveProtoPath("/var/protos/game.proto")).toBe(
      "/var/protos/game.proto",
    );
  });

  it("resolves canonical import paths against the service root", () => {
    expect(resolveProtoPath("projects/game/game.proto")).toBe(
      path.join(SERVICE_ROOT, "projects", "game", "game.proto"),
    );
  });
});

describe("createGrpcRuntime().client", () => {
  let loadSync: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    loadSync = vi.fn(
      (_filename: string | string[], _options?: protoLoader.Options) =>
        ({}) as protoLoader.PackageDefinition,
    );
  });

  it("loads canonical path with locked options, shared channel", () => {
    const constructions: Construction[] = [];
    const Service = fakeServiceClass(constructions);
    const proto = {
      projects: { game: { MemoryService: Service } },
    } as unknown as grpc.GrpcObject;
    const channel = { marker: "channel" } as unknown as grpc.Channel;
    const get = vi.fn(() => channel);
    const closeAll = vi.fn();
    const pool = { get, closeAll, size: 0 } as unknown as ChannelPool;
    const loadPackageDefinition = vi.fn(
      (_definition: protoLoader.PackageDefinition) => proto,
    );
    const handle = createGrpcRuntime({ loadSync, loadPackageDefinition, pool });

    const client = handle.runtime.client<unknown>({
      target: TARGET,
      protoPath: "projects/game/game.proto",
      serviceName: "projects.game.MemoryService",
    });

    expect(loadSync).toHaveBeenCalledTimes(1);
    expect(loadSync).toHaveBeenCalledWith(
      path.join(SERVICE_ROOT, "projects", "game", "game.proto"),
      {
        // Lockstep with ts_proto_library (tools/dev/js/ts_proto_library.bzl):
        // longs=String, enums=String, defaults, oneofs; keepCase omitted.
        longs: String,
        enums: String,
        defaults: true,
        oneofs: true,
        includeDirs: [SERVICE_ROOT],
      },
    );
    expect(get).toHaveBeenCalledWith(TARGET);
    expect(constructions).toHaveLength(1);
    expect(constructions[0].address).toBe(TARGET);
    expect(constructions[0].options).toEqual({ channelOverride: channel });
    expect(client).toBeInstanceOf(Service);
  });

  it("uses an absolute protoPath directly", () => {
    const constructions: Construction[] = [];
    const { handle, get } = harness(
      loadSync,
      serviceProto(constructions),
    );

    handle.runtime.client({
      target: TARGET,
      protoPath: "/opt/protos/game.proto",
      serviceName: "projects.game.MemoryService",
    });

    expect(loadSync).toHaveBeenCalledWith("/opt/protos/game.proto", {
      longs: String,
      enums: String,
      defaults: true,
      oneofs: true,
      includeDirs: [SERVICE_ROOT],
    });
    expect(constructions).toHaveLength(1);
    expect(get).toHaveBeenCalledWith(TARGET);
  });

  it("caches the package definition per protoPath + includeDirs", () => {
    const constructions: Construction[] = [];
    const { handle } = harness(loadSync, serviceProto(constructions));

    const options = {
      target: TARGET,
      protoPath: "projects/game/game.proto",
      serviceName: "projects.game.MemoryService",
    };
    handle.runtime.client(options);
    handle.runtime.client(options);

    // Same cache key: one loadSync, but a fresh client per call.
    expect(loadSync).toHaveBeenCalledTimes(1);
    expect(constructions).toHaveLength(2);

    // A different include set is a different cache key.
    handle.runtime.client({ ...options, includeDirs: ["/other/root"] });
    expect(loadSync).toHaveBeenCalledTimes(2);
    expect(loadSync).toHaveBeenLastCalledWith(
      path.join(SERVICE_ROOT, "projects", "game", "game.proto"),
      expect.objectContaining({ includeDirs: ["/other/root"] }),
    );
  });

  it("fails loud and identifies the request by label", () => {
    const { handle } = harness(loadSync, {} as grpc.GrpcObject);

    // Default label = serviceName.
    expect(() =>
      handle.runtime.client({
        target: TARGET,
        protoPath: "projects/game/game.proto",
        serviceName: "projects.game.Nope",
      }),
    ).toThrow(/service "projects.game.Nope" not found/);

    // An explicit label replaces the identifier in the message.
    expect(() =>
      handle.runtime.client({
        target: TARGET,
        protoPath: "projects/game/game.proto",
        serviceName: "projects.game.Nope",
        label: "memory client",
      }),
    ).toThrow(/service "memory client" not found/);
  });

  it("fails loud on a non-constructor service name", () => {
    const { handle } = harness(
      loadSync,
      {
        projects: { game: { MemoryService: { format: "message" } } },
      } as unknown as grpc.GrpcObject,
    );

    expect(() =>
      handle.runtime.client({
        target: TARGET,
        protoPath: "projects/game/game.proto",
        serviceName: "projects.game.MemoryService",
      }),
    ).toThrow(/not a service client constructor/);
  });

  it("closes every cached channel through the handle teardown", () => {
    const { handle, closeAll } = harness(
      loadSync,
      serviceProto([]),
    );

    handle.close();

    expect(closeAll).toHaveBeenCalledTimes(1);
  });
});
