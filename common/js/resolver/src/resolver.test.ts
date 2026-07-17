import { describe, it, expect, vi } from "vitest";
import { createResolver } from "./resolver";
import { InvalidTargetError, MissingEnvironmentError, ServiceNotFoundError } from "./errors";
import type { Target } from "./target";

/**
 * Helper: creates a fake fetch that returns a JSON response.
 */
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

/**
 * Helper: creates a fake fetch that returns an HTTP error.
 */
function fakeFetchWithError(status: number, body = ""): ReturnType<typeof vi.fn> {
  return vi.fn(async () => ({
    ok: false,
    status,
    headers: new Headers(),
    redirected: false,
    statusText: "",
    type: "basic" as ResponseType,
    url: "",
    json: async () => {
      throw new Error("not json");
    },
    text: async () => body,
  } as unknown as Response));
}

/**
 * Standard test environment matching quickstart scenario 1.
 */
const TEST_ENV = {
  DOMINION_ENVIRONMENT: "dev.alpha",
  SERVICE_APP: "myapp",
};

describe("createResolver", () => {
  it("resolves numeric port target with filtered endpoints", async () => {
    const fetch = fakeFetchReturning({
      endpoints: ["10.0.0.1:50051", "10.0.0.2:50051", "10.0.0.3:9090"],
      ports: {},
      isStateful: false,
      statefulInstances: [],
    });

    const resolver = createResolver({ env: TEST_ENV, fetch });
    const result = await resolver.resolve("myapp/myservice:50051");

    expect(result).toEqual(["10.0.0.1:50051", "10.0.0.2:50051"]);
  });

  it("resolves a numeric port target (residual ports map ignored)", async () => {
    // Named-port resolution via a ports map was REMOVED: the grpc-js resolver
    // cannot identify the name part of a target, so `parseTarget` rejects
    // non-numeric ports (target.ts:28-29 "Named ports are not supported").
    // A residual `ports` field in the deploy response is ignored for numeric
    // targets; this verifies numeric filtering is unaffected by it.
    const fetch = fakeFetchReturning({
      endpoints: ["10.0.0.1:50051", "10.0.0.2:50051"],
      ports: { grpc: 50051 },
      isStateful: false,
      statefulInstances: [],
    });

    const resolver = createResolver({ env: TEST_ENV, fetch });
    const result = await resolver.resolve("myapp/myservice:50051");

    expect(result).toEqual(["10.0.0.1:50051", "10.0.0.2:50051"]);
  });

  it("strips dominion:/// scheme and resolves correctly", async () => {
    const fetch = fakeFetchReturning({
      endpoints: ["10.0.0.1:50051"],
      ports: {},
      isStateful: false,
      statefulInstances: [],
    });

    const resolver = createResolver({ env: TEST_ENV, fetch });
    const result = await resolver.resolve("dominion:///myapp/myservice:50051");

    expect(result).toEqual(["10.0.0.1:50051"]);
  });

  it("rejects invalid target string with InvalidTargetError", async () => {
    const fetch = fakeFetchReturning({});
    const resolver = createResolver({ env: TEST_ENV, fetch });

    await expect(resolver.resolve("invalid-target")).rejects.toThrow(InvalidTargetError);
  });

  it("throws MissingEnvironmentError when DOMINION_ENVIRONMENT is missing", async () => {
    const fetch = fakeFetchReturning({});
    const resolver = createResolver({
      env: { SERVICE_APP: "myapp" },
      fetch,
    });

    await expect(resolver.resolve("myapp/myservice:50051")).rejects.toThrow(
      MissingEnvironmentError,
    );
  });

  it("throws InvalidTargetError when SERVICE_APP mismatches target app", async () => {
    const fetch = fakeFetchReturning({});
    const resolver = createResolver({
      env: {
        DOMINION_ENVIRONMENT: "dev.alpha",
        SERVICE_APP: "otherapp",
      },
      fetch,
    });

    await expect(resolver.resolve("myapp/myservice:50051")).rejects.toThrow(InvalidTargetError);
  });

  it("throws ServiceNotFoundError when deploy API returns 404", async () => {
    const fetch = fakeFetchWithError(404);
    const resolver = createResolver({ env: TEST_ENV, fetch });

    await expect(resolver.resolve("myapp/myservice:50051")).rejects.toThrow(ServiceNotFoundError);
  });

  it("uses injected env and fetch with correct resource name in URL", async () => {
    const fetch = fakeFetchReturning({
      endpoints: ["10.0.0.1:50051"],
      ports: {},
      isStateful: false,
      statefulInstances: [],
    });

    const resolver = createResolver({ env: TEST_ENV, fetch });
    await resolver.resolve("myapp/myservice:50051");

    expect(fetch).toHaveBeenCalledOnce();
    const calledUrl = fetch.mock.calls[0][0] as string;
    expect(calledUrl).toBe(
      "http://infra.liukexin.com/v1/deploy/scopes/dev/environments/alpha/apps/myapp/services/myservice/endpoints",
    );
  });

  it("accepts Target object directly without string parsing", async () => {
    const fetch = fakeFetchReturning({
      endpoints: ["10.0.0.1:50051"],
      ports: {},
      isStateful: false,
      statefulInstances: [],
    });

    const target: Target = {
      app: "myapp",
      service: "myservice",
      port: { kind: "number", port: 50051 },
    };

    const resolver = createResolver({ env: TEST_ENV, fetch });
    const result = await resolver.resolve(target);

    expect(result).toEqual(["10.0.0.1:50051"]);
  });

  it("rejects a named port target with InvalidTargetError (named ports not supported)", async () => {
    const fetch = fakeFetchReturning({
      endpoints: ["10.0.0.1:1234"],
      ports: {}, // irrelevant: parseTarget rejects non-numeric ports before any port lookup
      isStateful: false,
      statefulInstances: [],
    });

    const resolver = createResolver({ env: TEST_ENV, fetch });

    await expect(resolver.resolve("myapp/myservice:grpc")).rejects.toThrow(InvalidTargetError);
  });

  it("returns sorted unique endpoints with duplicates", async () => {
    const fetch = fakeFetchReturning({
      endpoints: ["10.0.0.2:50051", "10.0.0.1:50051", "10.0.0.2:50051"],
      ports: {},
      isStateful: false,
      statefulInstances: [],
    });

    const resolver = createResolver({ env: TEST_ENV, fetch });
    const result = await resolver.resolve("myapp/myservice:50051");

    expect(result).toEqual(["10.0.0.1:50051", "10.0.0.2:50051"]);
  });

  it("uses default config values when no config is provided", () => {
    // Verify the factory doesn't throw when config is omitted.
    // Actual network call is not made; just testing construction.
    const resolver = createResolver();
    expect(typeof resolver.resolve).toBe("function");
  });
});
