import { describe, it, expect, vi } from "vitest";
import { createDeployClient, DeployClient } from "./deploy-client";
import { DeployServiceError, ServiceNotFoundError } from "./errors";
import type { ServiceEndpoints } from "./types";

/** Helper: create a fake Response with the given status and JSON body. */
function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

/** Helper: create a fake Response with the given status and plain text body. */
function textResponse(status: number, body: string): Response {
  return new Response(body, {
    status,
    headers: { "Content-Type": "text/plain" },
  });
}

describe("createDeployClient", () => {
  describe("successful fetch", () => {
    it("returns parsed ServiceEndpoints with camelCase fields", async () => {
      const fakeFetch = vi.fn().mockResolvedValue(
        jsonResponse(200, {
          endpoints: ["10.0.0.1:50051", "10.0.0.2:50051"],
          ports: { grpc: 50051 },
          isStateful: false,
          statefulInstances: [],
        }),
      );

      const client = createDeployClient({ fetch: fakeFetch });
      const result = await client.getServiceEndpoints(
        "deploy/scopes/prod/environments/staging/apps/myapp/services/mysvc/endpoints",
      );

      expect(result).toEqual<ServiceEndpoints>({
        endpoints: ["10.0.0.1:50051", "10.0.0.2:50051"],
        ports: { grpc: 50051 },
        isStateful: false,
        statefulInstances: [],
      });

      expect(fakeFetch).toHaveBeenCalledOnce();
      expect(fakeFetch).toHaveBeenCalledWith(
        "http://infra.liukexin.com/v1/deploy/scopes/prod/environments/staging/apps/myapp/services/mysvc/endpoints",
        expect.objectContaining({ signal: expect.any(AbortSignal) }),
      );
    });

    it("maps snake_case proto-JSON fields to camelCase", async () => {
      const fakeFetch = vi.fn().mockResolvedValue(
        jsonResponse(200, {
          endpoints: ["10.0.0.1:50051"],
          ports: { grpc: 50051, http: 8080 },
          is_stateful: true,
          stateful_instances: [
            { index: 0, endpoints: ["10.0.0.1:50051"], hostname: "instance-0" },
            { index: 1, endpoints: ["10.0.0.2:50051"], hostname: "instance-1" },
          ],
        }),
      );

      const client = createDeployClient({ fetch: fakeFetch });
      const result = await client.getServiceEndpoints("deploy/scopes/test/endpoints");

      expect(result.isStateful).toBe(true);
      expect(result.statefulInstances).toEqual([
        { index: 0, endpoints: ["10.0.0.1:50051"], hostname: "instance-0" },
        { index: 1, endpoints: ["10.0.0.2:50051"], hostname: "instance-1" },
      ]);
    });

    it("ignores unknown extra fields in response", async () => {
      const fakeFetch = vi.fn().mockResolvedValue(
        jsonResponse(200, {
          endpoints: ["10.0.0.1:50051"],
          ports: { grpc: 50051 },
          isStateful: false,
          statefulInstances: [],
          // Unknown proto-JSON fields
          unknownField: "should be ignored",
          "@type": "type.googleapis.com/deploy.ServiceEndpoints",
          metadata: { version: "v1" },
        }),
      );

      const client = createDeployClient({ fetch: fakeFetch });
      const result = await client.getServiceEndpoints("deploy/scopes/test/endpoints");

      expect(result).toEqual<ServiceEndpoints>({
        endpoints: ["10.0.0.1:50051"],
        ports: { grpc: 50051 },
        isStateful: false,
        statefulInstances: [],
      });
    });
  });

  describe("HTTP 404", () => {
    it("throws ServiceNotFoundError with resource name", async () => {
      const fakeFetch = vi.fn().mockResolvedValue(
        textResponse(404, "not found"),
      );

      const client = createDeployClient({ fetch: fakeFetch });

      await expect(
        client.getServiceEndpoints("deploy/scopes/prod/apps/myapp/services/missing/endpoints"),
      ).rejects.toThrow(ServiceNotFoundError);

      await expect(
        client.getServiceEndpoints("deploy/scopes/prod/apps/myapp/services/missing/endpoints"),
      ).rejects.toThrow("service not found: deploy/scopes/prod/apps/myapp/services/missing/endpoints");
    });
  });

  describe("HTTP 500", () => {
    it("throws DeployServiceError with status code and body text", async () => {
      const fakeFetch = vi.fn().mockResolvedValue(
        textResponse(500, "internal server error"),
      );

      const client = createDeployClient({ fetch: fakeFetch });

      try {
        await client.getServiceEndpoints("deploy/scopes/test/endpoints");
        expect.unreachable("should have thrown");
      } catch (err) {
        expect(err).toBeInstanceOf(DeployServiceError);
        expect(err).not.toBeInstanceOf(ServiceNotFoundError);
        expect((err as DeployServiceError).message).toContain("500");
        expect((err as DeployServiceError).message).toContain("internal server error");
      }
    });
  });

  describe("network error", () => {
    it("throws DeployServiceError wrapping the original error", async () => {
      const networkError = new TypeError("Failed to fetch");
      const fakeFetch = vi.fn().mockRejectedValue(networkError);

      const client = createDeployClient({ fetch: fakeFetch });

      try {
        await client.getServiceEndpoints("deploy/scopes/test/endpoints");
        expect.unreachable("should have thrown");
      } catch (err) {
        expect(err).toBeInstanceOf(DeployServiceError);
        expect((err as DeployServiceError).message).toContain("deploy service request failed");
        expect((err as DeployServiceError).message).toContain("Failed to fetch");
      }
    });
  });

  describe("request timeout", () => {
    it("throws DeployServiceError when the request exceeds requestTimeoutMs", async () => {
      // Simulate undici behavior: the fetch hangs until the injected signal
      // aborts, then rejects with the signal's TimeoutError reason.
      const fakeFetch = vi.fn(
        (_url: string, init?: RequestInit) =>
          new Promise<Response>((_resolve, reject) => {
            init?.signal?.addEventListener("abort", () => {
              reject(init.signal!.reason);
            });
          }),
      );

      const client = createDeployClient({ fetch: fakeFetch, requestTimeoutMs: 50 });

      await expect(
        client.getServiceEndpoints("deploy/scopes/test/endpoints"),
      ).rejects.toThrow(DeployServiceError);

      try {
        await client.getServiceEndpoints("deploy/scopes/test/endpoints");
        expect.unreachable("should have thrown");
      } catch (err) {
        expect((err as DeployServiceError).message).toBe(
          "deploy service request timed out after 50ms",
        );
      }
    });
  });

  describe("defaults", () => {
    it("uses default deployBaseUrl and globalThis.fetch when not provided", async () => {
      const fakeFetch = vi.fn().mockResolvedValue(
        jsonResponse(200, {
          endpoints: ["10.0.0.1:50051"],
          ports: { grpc: 50051 },
          isStateful: false,
          statefulInstances: [],
        }),
      );

      const originalFetch = globalThis.fetch;
      (globalThis as Record<string, unknown>).fetch = fakeFetch;

      try {
        const client = createDeployClient();
        const result = await client.getServiceEndpoints("deploy/scopes/test/endpoints");

        expect(fakeFetch).toHaveBeenCalledWith(
          "http://infra.liukexin.com/v1/deploy/scopes/test/endpoints",
          expect.objectContaining({ signal: expect.any(AbortSignal) }),
        );
        expect(result.endpoints).toEqual(["10.0.0.1:50051"]);
      } finally {
        (globalThis as Record<string, unknown>).fetch = originalFetch;
      }
    });

    it("uses custom deployBaseUrl when provided", async () => {
      const fakeFetch = vi.fn().mockResolvedValue(
        jsonResponse(200, {
          endpoints: [],
          ports: {},
          isStateful: false,
          statefulInstances: [],
        }),
      );

      const client = createDeployClient({
        deployBaseUrl: "http://custom-deploy.example.com",
        fetch: fakeFetch,
      });

      await client.getServiceEndpoints("some/resource");

      expect(fakeFetch).toHaveBeenCalledWith(
        "http://custom-deploy.example.com/v1/some/resource",
        expect.objectContaining({ signal: expect.any(AbortSignal) }),
      );
    });
  });

  describe("named port map parsing", () => {
    it("parses named port map from response JSON", async () => {
      const fakeFetch = vi.fn().mockResolvedValue(
        jsonResponse(200, {
          endpoints: ["10.0.0.1:50051", "10.0.0.1:8080"],
          ports: { grpc: 50051, http: 8080, admin: 9090 },
          isStateful: false,
          statefulInstances: [],
        }),
      );

      const client = createDeployClient({ fetch: fakeFetch });
      const result = await client.getServiceEndpoints("deploy/scopes/test/endpoints");

      expect(result.ports).toEqual({ grpc: 50051, http: 8080, admin: 9090 });
    });
  });

  describe("statefulInstances parsing", () => {
    it("parses statefulInstances array with index/endpoints/hostname", async () => {
      const fakeFetch = vi.fn().mockResolvedValue(
        jsonResponse(200, {
          endpoints: ["10.0.0.1:50051", "10.0.0.2:50051", "10.0.0.3:50051"],
          ports: { grpc: 50051 },
          is_stateful: true,
          stateful_instances: [
            { index: 0, endpoints: ["10.0.0.1:50051"], hostname: "pod-0" },
            { index: 1, endpoints: ["10.0.0.2:50051"] },
            { index: 2, endpoints: ["10.0.0.3:50051"], hostname: "pod-2" },
          ],
        }),
      );

      const client = createDeployClient({ fetch: fakeFetch });
      const result = await client.getServiceEndpoints("deploy/scopes/test/endpoints");

      expect(result.isStateful).toBe(true);
      expect(result.statefulInstances).toHaveLength(3);
      expect(result.statefulInstances[0]).toEqual({
        index: 0,
        endpoints: ["10.0.0.1:50051"],
        hostname: "pod-0",
      });
      expect(result.statefulInstances[1]).toEqual({
        index: 1,
        endpoints: ["10.0.0.2:50051"],
      });
      expect(result.statefulInstances[1].hostname).toBeUndefined();
      expect(result.statefulInstances[2]).toEqual({
        index: 2,
        endpoints: ["10.0.0.3:50051"],
        hostname: "pod-2",
      });
    });
  });
});
