import { beforeEach, describe, expect, it, vi } from "vitest";

import type { EndpointResolver } from "@dominion/common-js-resolver";

import { createRedisClient } from "./client.js";

const { createClientMock, connectMock } = vi.hoisted(() => ({
  createClientMock: vi.fn(),
  connectMock: vi.fn(async () => undefined),
}));

// The contract fixes the public options to resolver/env only, leaving no DI
// seam for the third-party client (node-redis is CJS and is loaded through the
// default import per style/javascript.md §CJS 第三方依赖导入约定). The module is
// therefore mocked here, and the mock is asserted positively on every path
// (style/javascript.md §规则：验证 mock 确实生效).
vi.mock("redis", () => ({
  default: { createClient: createClientMock },
}));

function fakeResolver(endpoints: string[]): EndpointResolver & {
  resolve: ReturnType<typeof vi.fn>;
} {
  return { resolve: vi.fn(async () => endpoints) };
}

beforeEach(() => {
  vi.clearAllMocks();
  connectMock.mockResolvedValue(undefined);
  createClientMock.mockReturnValue({ connect: connectMock });
});

describe("createRedisClient", () => {
  it("resolves the endpoint, derives the instance password and connects", async () => {
    const resolver = fakeResolver(["10.0.0.12:6379", "10.0.0.13:6379"]);

    const client = await createRedisClient("myapp/myredis", {
      resolver,
      env: { DOMINION_ENVIRONMENT: "dev" },
    });

    expect(resolver.resolve).toHaveBeenCalledWith("myapp/myredis:6379");
    expect(createClientMock).toHaveBeenCalledWith({
      socket: { host: "10.0.0.12", port: 6379 },
      password: "k1OTVRTOdkt9XeuhOVEk5yR82ZZqnjxt",
    });
    expect(connectMock).toHaveBeenCalledOnce();
    expect(client).toBe(createClientMock.mock.results[0]!.value);
  });

  it("falls back to the default environment when DOMINION_ENVIRONMENT is missing", async () => {
    const resolver = fakeResolver(["10.0.0.12:6379"]);

    await createRedisClient("myapp/myredis", { resolver, env: {} });

    expect(createClientMock).toHaveBeenCalledWith({
      socket: { host: "10.0.0.12", port: 6379 },
      password: "FF53SVZiUNQCFp1k4B2c0fp4P4NCM729",
    });
  });

  it("rejects an invalid target and never resolves or builds a client", async () => {
    const resolver = fakeResolver(["10.0.0.12:6379"]);

    const call = createRedisClient("myapp", { resolver, env: {} });

    await expect(call).rejects.toThrow(/"app\/name"/);
    expect(resolver.resolve).not.toHaveBeenCalled();
    expect(createClientMock).not.toHaveBeenCalled();
  });

  it("rejects when the resolver returns no ready endpoints", async () => {
    const resolver = fakeResolver([]);

    const call = createRedisClient("myapp/myredis", { resolver, env: {} });

    await expect(call).rejects.toThrow(/myapp\/myredis/);
    expect(createClientMock).not.toHaveBeenCalled();
  });

  it("propagates the node-redis connection error unchanged", async () => {
    const resolver = fakeResolver(["10.0.0.12:6379"]);
    const driverError = new Error("connection refused");
    connectMock.mockRejectedValueOnce(driverError);

    const call = createRedisClient("myapp/myredis", {
      resolver,
      env: { DOMINION_ENVIRONMENT: "dev" },
    });

    await expect(call).rejects.toBe(driverError);
  });
});
