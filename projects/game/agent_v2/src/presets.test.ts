import { describe, expect, it, vi } from "vitest";
import { ENV_DOMINION_ENVIRONMENT } from "@dominion/common-js-constants";
import type { EndpointResolver } from "@dominion/common-js-resolver";

import { MONGO_TARGET, resolveMongoUri } from "./presets.js";

/**
 * The deployment Mongo credential resolution (the agent_v2 side of the
 * preset persistence wiring, T006): endpoint precedence and the deterministic
 * credential derivation. The derivation vectors are same-source with
 * dominion/common/gopkg/mongo/credentials.go (pinned by that package's
 * client_test.go); the store/document behavior lives with the authoring
 * plugin's Mongo store tests.
 */

describe("resolveMongoUri", () => {
  it("prefers a direct MONGO_URI without contacting the resolver", async () => {
    const resolve = vi.fn(async () => ["10.0.0.9:27017"]);

    const uri = await resolveMongoUri({
      env: { MONGO_URI: "mongodb://direct:27017" },
      resolver: { resolve } as unknown as EndpointResolver,
    });

    expect(uri).toBe("mongodb://direct:27017");
    expect(resolve).not.toHaveBeenCalled();
  });

  it("resolves the Dominion mongo target into a credentialed mongodb URI", async () => {
    const resolve = vi.fn(async () => ["10.0.0.9:27017"]);

    const uri = await resolveMongoUri({
      env: {},
      resolver: { resolve } as unknown as EndpointResolver,
    });

    expect(resolve).toHaveBeenCalledWith(MONGO_TARGET);
    // The password is the deterministic deployment derivation (same-source
    // with dominion/common/gopkg/mongo/credentials.go; the cross-implementation
    // match is pinned by that package's client_test.go vectors) for the
    // default environment.
    expect(uri).toBe(
      "mongodb://admin:JaOE4KM29XdamfOs9zUqhC2QHavC2UJn@10.0.0.9:27017/admin?authSource=admin",
    );
  });

  it("derives the credential from DOMINION_ENVIRONMENT", async () => {
    const resolve = vi.fn(async () => ["10.0.0.9:27017"]);

    const uri = await resolveMongoUri({
      env: { [ENV_DOMINION_ENVIRONMENT]: "test-env" },
      resolver: { resolve } as unknown as EndpointResolver,
    });

    expect(uri).toBe(
      "mongodb://admin:iiG62he1f7TPRHuY7ooNT2uVfVgJ4fKN@10.0.0.9:27017/admin?authSource=admin",
    );
  });

  it("fails loud when the resolver returns no endpoints", async () => {
    const resolve = vi.fn(async () => []);

    await expect(
      resolveMongoUri({ env: {}, resolver: { resolve } as unknown as EndpointResolver }),
    ).rejects.toThrow(/no endpoints/);
  });
});
