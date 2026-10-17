import { beforeEach, describe, expect, it, vi } from "vitest";

import type { EndpointResolver } from "@dominion/common-js-resolver";

import { createMongoClient, resolveMongoUri } from "./client.js";

const { MongoClientMock, connectMock } = vi.hoisted(() => {
  const connect = vi.fn(async () => undefined);
  const client = vi.fn(function (this: { connect: typeof connect }) {
    this.connect = connect;
  });
  return { MongoClientMock: client, connectMock: connect };
});

// The contract fixes the public options to resolver/env only, leaving no DI
// seam for the third-party client. `mongodb` is a verified CJS named-import
// package (style/javascript.md §CJS 第三方依赖导入约定) and is mocked here; the
// mock is asserted positively on every path (style/javascript.md §规则：验证
// mock 确实生效).
vi.mock("mongodb", () => ({ MongoClient: MongoClientMock }));

function fakeResolver(endpoints: string[]): EndpointResolver & {
  resolve: ReturnType<typeof vi.fn>;
} {
  return { resolve: vi.fn(async () => endpoints) };
}

beforeEach(() => {
  vi.clearAllMocks();
  connectMock.mockResolvedValue(undefined);
});

describe("resolveMongoUri", () => {
  it("normalizes app/name, resolves the endpoint and builds the credentialed URI", async () => {
    const resolver = fakeResolver(["10.0.0.12:27017", "10.0.0.13:27017"]);

    const uri = await resolveMongoUri("myapp/mycollection", {
      resolver,
      env: { DOMINION_ENVIRONMENT: "dev" },
    });

    expect(resolver.resolve).toHaveBeenCalledWith("myapp/mycollection:27017");
    expect(uri).toBe(
      "mongodb://admin:0yzhQyFYKdateABPUyUCpShTIbDqKhhm@10.0.0.12:27017/admin?authSource=admin",
    );
  });

  it("normalizes the dominion:///app/service:port form to app/service:port", async () => {
    const resolver = fakeResolver(["10.0.0.9:27017"]);

    const uri = await resolveMongoUri("dominion:///game/mongo:27017", {
      resolver,
      env: {},
    });

    expect(resolver.resolve).toHaveBeenCalledWith("game/mongo:27017");
    expect(uri).toBe(
      "mongodb://admin:JaOE4KM29XdamfOs9zUqhC2QHavC2UJn@10.0.0.9:27017/admin?authSource=admin",
    );
  });

  it("falls back to the default environment when DOMINION_ENVIRONMENT is missing", async () => {
    const resolver = fakeResolver(["10.0.0.12:27017"]);

    const uri = await resolveMongoUri("myapp/mycollection", { resolver, env: {} });

    expect(uri).toBe(
      "mongodb://admin:z4HVlRoJrVIjrgkNmXqzsWTki23p4MXb@10.0.0.12:27017/admin?authSource=admin",
    );
  });

  it("rejects an invalid target and never resolves", async () => {
    const resolver = fakeResolver(["10.0.0.12:27017"]);

    const call = resolveMongoUri("myapp", { resolver, env: {} });

    await expect(call).rejects.toThrow(/"app\/name"/);
    expect(resolver.resolve).not.toHaveBeenCalled();
  });

  it("rejects when the resolver returns no ready endpoints", async () => {
    const resolver = fakeResolver([]);

    const call = resolveMongoUri("myapp/mycollection", { resolver, env: {} });

    await expect(call).rejects.toThrow(/myapp\/mycollection/);
  });
});

describe("createMongoClient", () => {
  it("connects the driver with the resolved credentialed URI", async () => {
    const resolver = fakeResolver(["10.0.0.9:27017"]);

    const client = await createMongoClient("dominion:///game/mongo:27017", {
      resolver,
      env: {},
    });

    expect(MongoClientMock).toHaveBeenCalledWith(
      "mongodb://admin:JaOE4KM29XdamfOs9zUqhC2QHavC2UJn@10.0.0.9:27017/admin?authSource=admin",
    );
    expect(connectMock).toHaveBeenCalledOnce();
    expect(client).toBe(MongoClientMock.mock.results[0]!.value);
  });

  it("propagates the driver connection error unchanged", async () => {
    const resolver = fakeResolver(["10.0.0.9:27017"]);
    const driverError = new Error("connection refused");
    connectMock.mockRejectedValueOnce(driverError);

    const call = createMongoClient("dominion:///game/mongo:27017", {
      resolver,
      env: {},
    });

    await expect(call).rejects.toBe(driverError);
  });
});
