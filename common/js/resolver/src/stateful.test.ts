import { describe, it, expect } from "vitest";
import {
  InvalidTargetError,
  MissingEnvironmentError,
  ServiceNotStatefulError,
  StatefulInstanceNoReadyEndpointsError,
  StatefulInstanceNotFoundError,
} from "./errors";
import { createStatefulResolver } from "./stateful";

/**
 * Helper to build a fake fetch that returns the given JSON body.
 */
function fakeFetch(body: unknown): (input: string) => Promise<Response> {
  return async (input: string) => {
    return new Response(JSON.stringify(body), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  };
}

/**
 * Default test environment matching quickstart scenario 3.
 */
const TEST_ENV = {
  DOMINION_ENVIRONMENT: "dev.alpha",
  SERVICE_APP: "myapp",
};

/**
 * Quickstart scenario 3: fake deploy response for a stateful service with 3
 * instances. Endpoints are published on the numeric target port directly.
 *
 * NOTE: an earlier revision used a named port (`grpc`) with a `ports` remap
 * (`1234` → `50051`). Named-port support was REMOVED because the grpc-js
 * resolver cannot identify the name part of a target, so `parseTarget`
 * rejects non-numeric ports (see `target.ts:28-29` "Named ports are not
 * supported"). Tests therefore exercise numeric ports only.
 */
function statefulDeployBody() {
  return {
    isStateful: true,
    statefulInstances: [
      { index: 0, endpoints: ["10.0.0.10:50051"] },
      { index: 1, endpoints: ["10.0.0.11:50051"] },
      { index: 2, endpoints: ["10.0.0.12:50051"] },
    ],
    ports: {},
  };
}

describe("createStatefulResolver", () => {
  it("resolves instance 1 of a stateful service (quickstart scenario 3)", async () => {
    const resolver = createStatefulResolver({
      env: TEST_ENV,
      fetch: fakeFetch(statefulDeployBody()),
    });

    const endpoints = await resolver.resolveInstance(
      "myapp/myservice:50051",
      1,
    );

    expect(endpoints).toEqual(["10.0.0.11:50051"]);
  });

  it("throws StatefulInstanceNotFoundError for missing instance index", async () => {
    const resolver = createStatefulResolver({
      env: TEST_ENV,
      fetch: fakeFetch(statefulDeployBody()),
    });

    await expect(
      resolver.resolveInstance("myapp/myservice:50051", 99),
    ).rejects.toThrow(StatefulInstanceNotFoundError);
  });

  it("throws StatefulInstanceNoReadyEndpointsError when instance has no matching endpoints", async () => {
    const body = {
      isStateful: true,
      statefulInstances: [
        { index: 0, endpoints: ["10.0.0.10:1234"] },
        { index: 1, endpoints: [] },
        { index: 2, endpoints: ["10.0.0.12:1234"] },
      ],
      ports: {},
    };

    const resolver = createStatefulResolver({
      env: TEST_ENV,
      fetch: fakeFetch(body),
    });

    await expect(
      resolver.resolveInstance("myapp/myservice:50051", 1),
    ).rejects.toThrow(StatefulInstanceNoReadyEndpointsError);
  });

  it("throws ServiceNotStatefulError for non-stateful service", async () => {
    const body = {
      isStateful: false,
      endpoints: ["10.0.0.1:50051"],
      ports: {},
      statefulInstances: [],
    };

    const resolver = createStatefulResolver({
      env: TEST_ENV,
      fetch: fakeFetch(body),
    });

    await expect(
      resolver.resolveInstance("myapp/myservice:50051", 0),
    ).rejects.toThrow(ServiceNotStatefulError);
  });

  it("resolves a numeric port on a multi-port stateful instance", async () => {
    const body = {
      isStateful: true,
      statefulInstances: [
        {
          index: 0,
          endpoints: ["10.0.0.10:8080", "10.0.0.10:9090"],
        },
      ],
      ports: {},
    };

    const resolver = createStatefulResolver({
      env: TEST_ENV,
      fetch: fakeFetch(body),
    });

    // Numeric port 9090 — filters the instance endpoints by exact port match.
    const endpoints = await resolver.resolveInstance(
      "myapp/myservice:9090",
      0,
    );
    expect(endpoints).toEqual(["10.0.0.10:9090"]);
  });

  it("validates SERVICE_APP before calling deploy API", async () => {
    let fetchCalled = false;
    const fetch = async () => {
      fetchCalled = true;
      return new Response("{}", { status: 200 });
    };

    const resolver = createStatefulResolver({
      env: {
        DOMINION_ENVIRONMENT: "dev.alpha",
        SERVICE_APP: "otherapp",
      },
      fetch,
    });

    // Target app "myapp" does not match SERVICE_APP "otherapp"
    await expect(
      resolver.resolveInstance("myapp/myservice:50051", 0),
    ).rejects.toThrow(InvalidTargetError);

    expect(fetchCalled).toBe(false);
  });

  it("uses injected env and fetch; calls correct deploy URL", async () => {
    let calledUrl = "";
    const body = statefulDeployBody();
    const fetch = async (input: string) => {
      calledUrl = input;
      return new Response(JSON.stringify(body), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    };

    const resolver = createStatefulResolver({
      env: TEST_ENV,
      fetch,
      deployBaseUrl: "http://custom-deploy.example.com",
    });

    await resolver.resolveInstance("myapp/myservice:50051", 1);

    expect(calledUrl).toBe(
      "http://custom-deploy.example.com/v1/deploy/scopes/dev/environments/alpha/apps/myapp/services/myservice/endpoints",
    );
  });

  it("throws MissingEnvironmentError when DOMINION_ENVIRONMENT is missing", async () => {
    const resolver = createStatefulResolver({
      env: { SERVICE_APP: "myapp" },
      fetch: fakeFetch(statefulDeployBody()),
    });

    await expect(
      resolver.resolveInstance("myapp/myservice:50051", 0),
    ).rejects.toThrow(MissingEnvironmentError);
  });

  it("accepts a parsed Target object instead of a string", async () => {
    const resolver = createStatefulResolver({
      env: TEST_ENV,
      fetch: fakeFetch(statefulDeployBody()),
    });

    const endpoints = await resolver.resolveInstance(
      { app: "myapp", service: "myservice", port: { kind: "number", port: 50051 } },
      2,
    );

    expect(endpoints).toEqual(["10.0.0.12:50051"]);
  });
});
