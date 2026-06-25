/**
 * Tests for PromptClient.
 *
 * Uses dependency injection to pass a mock gRPC client, verifying the
 * PromptClient correctly wraps the GetAgentProfile RPC call.
 */
import { describe, it, expect, vi, beforeEach } from "vitest";
import { PromptClient } from "./prompt-client";

// ---------------------------------------------------------------------------
// Mock the modules that PromptClient imports during construction
// ---------------------------------------------------------------------------

// For dependency injection we construct PromptClient with a mock client,
// but we still mock the modules so the constructor doesn't throw when
// loading proto files etc.
const { MockClient } = vi.hoisted(() => {
  const MockClient = vi.fn(
    (_target: string, _creds: unknown, _options: Record<string, unknown>) => ({
      getAgentProfile: vi.fn(),
      close: vi.fn(),
    }),
  );
  return { MockClient };
});

vi.mock("@grpc/grpc-js", () => {
  // A simple constructor that accepts (target, credentials) and returns
  // an object with a mockable getAgentProfile method.
  return {
    Client: MockClient,
    loadPackageDefinition: vi.fn(() => ({
      projects: {
        game: {
          PromptService: MockClient,
        },
      },
    })),
    credentials: {
      createInsecure: vi.fn(() => ({ type: "insecure" })),
      createSsl: vi.fn(() => ({ type: "ssl" })),
    },
    status: {
      NOT_FOUND: 5,
      OK: 0,
      UNAVAILABLE: 14,
    },
    connectivityState: {
      IDLE: 0,
      CONNECTING: 1,
      READY: 2,
      TRANSIENT_FAILURE: 3,
      SHUTDOWN: 4,
    },
  };
});

vi.mock("node:fs", () => ({
  existsSync: vi.fn(() => true),
  readFileSync: vi.fn(() => Buffer.from("fake-ca-cert")),
}));

vi.mock("@grpc/proto-loader", () => ({
  loadSync: vi.fn(() => ({})),
}));

vi.mock("@dominion/common-js-grpc-resolver", () => ({
  registerDominionResolver: vi.fn(),
  createDeployClient: vi.fn(() => ({
    getServiceEndpoints: vi.fn(),
  })),
}));

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("PromptClient", () => {
  let mockClient: {
    getAgentProfile: ReturnType<typeof vi.fn>;
    close: ReturnType<typeof vi.fn>;
  };

  beforeEach(() => {
    // Create a fresh mock client for each test
    mockClient = {
      getAgentProfile: vi.fn(),
      close: vi.fn(),
    };
    MockClient.mockClear();
  });

  describe("getProfile", () => {
    it("returns model and systemPrompt for a valid profile", async () => {
      const profileName = "my-profile";
      const expectedModel = "opencode-go/deepseek-v4-pro";
      const expectedSystemPrompt = "You are a helpful assistant.";

      mockClient.getAgentProfile.mockImplementation(
        (
          _req: { agentProfileName: string },
          cb: (err: null, response: { model: string; systemPrompt: string; toolNames: string[] }) => void,
        ) => {
          cb(null, {
            model: expectedModel,
            systemPrompt: expectedSystemPrompt,
            toolNames: ["mouse"],
          });
        },
      );

      const client = new PromptClient(mockClient as any);
      const result = await client.getProfile(profileName);

      expect(result).toEqual({
        model: expectedModel,
        systemPrompt: expectedSystemPrompt,
        toolNames: ["mouse"],
      });
    });

    it("throws with NOT_FOUND for a missing profile", async () => {
      const profileName = "nonexistent-profile";

      const notFoundError = Object.assign(
        new Error("Agent profile not found"),
        { code: 5 }, // grpc.status.NOT_FOUND
      );

      mockClient.getAgentProfile.mockImplementation(
        (
          _req: { agentProfileName: string },
          cb: (err: Error, response: null) => void,
        ) => {
          cb(notFoundError, null);
        },
      );

      const client = new PromptClient(mockClient as any);

      await expect(client.getProfile(profileName)).rejects.toThrow(
        "Agent profile not found",
      );
    });

    it("correctly extracts model and systemPrompt from the response", async () => {
      const profileName = "data-profile";
      const model = "opencode-go/deepseek-v4";
      const systemPrompt = "You are a code assistant.";

      mockClient.getAgentProfile.mockImplementation(
        (
          _req: { agentProfileName: string },
          cb: (
            err: null,
            response: { model: string; systemPrompt: string; toolNames: string[] },
          ) => void,
        ) => {
          cb(null, { model, systemPrompt, toolNames: [] });
        },
      );

      const client = new PromptClient(mockClient as any);
      const result = await client.getProfile(profileName);

      expect(result.model).toBe(model);
      expect(result.systemPrompt).toBe(systemPrompt);
      expect(result.toolNames).toEqual([]);

      expect(mockClient.getAgentProfile).toHaveBeenCalledTimes(1);
      expect(mockClient.getAgentProfile).toHaveBeenCalledWith(
        { agentProfileName: profileName },
        expect.any(Function),
      );
    });

    it("propagates non-NOT_FOUND gRPC errors", async () => {
      const profileName = "error-profile";

      const error = Object.assign(
        new Error("Service unavailable"),
        { code: 14 }, // grpc.status.UNAVAILABLE
      );

      mockClient.getAgentProfile.mockImplementation(
        (
          _req: { agentProfileName: string },
          cb: (err: Error, response: null) => void,
        ) => {
          cb(error, null);
        },
      );

      const client = new PromptClient(mockClient as any);

      await expect(client.getProfile(profileName)).rejects.toThrow(
        "Service unavailable",
      );
    });

    it("extracts toolNames from the response", async () => {
      mockClient.getAgentProfile.mockImplementation(
        (
          _req: { agentProfileName: string },
          cb: (
            err: null,
            response: { model: string; systemPrompt: string; toolNames: string[] },
          ) => void,
        ) => {
          cb(null, {
            model: "m",
            systemPrompt: "s",
            toolNames: ["mouse", "keyboard"],
          });
        },
      );

      const client = new PromptClient(mockClient as any);
      const result = await client.getProfile("tools-profile");

      expect(result.toolNames).toEqual(["mouse", "keyboard"]);
    });

    it("defaults toolNames to empty array when absent in response", async () => {
      mockClient.getAgentProfile.mockImplementation(
        (
          _req: { agentProfileName: string },
          cb: (
            err: null,
            response: { model: string; systemPrompt: string; toolNames?: string[] },
          ) => void,
        ) => {
          cb(null, { model: "m", systemPrompt: "s" });
        },
      );

      const client = new PromptClient(mockClient as any);
      const result = await client.getProfile("no-tools");

      expect(result.toolNames).toEqual([]);
    });
  });

  describe("close", () => {
    it("closes the underlying gRPC client", () => {
      const client = new PromptClient(mockClient as any);
      client.close();
      expect(mockClient.close).toHaveBeenCalledTimes(1);
    });
  });

  describe("channel construction", () => {
    beforeEach(() => {
      MockClient.mockClear();
    });

    it("configures keepalive and reconnect-backoff channel options", () => {
      new PromptClient();

      const options = MockClient.mock.calls[0]?.[2];
      expect(options?.["grpc.keepalive_time_ms"]).toBe(30_000);
      expect(options?.["grpc.keepalive_timeout_ms"]).toBe(10_000);
      expect(options?.["grpc.keepalive_permit_without_calls"]).toBe(1);
      expect(options?.["grpc.initial_reconnect_backoff_ms"]).toBe(1_000);
      expect(options?.["grpc.max_reconnect_backoff_ms"]).toBe(15_000);
    });
  });

  describe("warmup", () => {
    it("resolves true when the channel is already READY", async () => {
      const channel = {
        getConnectivityState: vi.fn(() => 2), // READY
        watchConnectivityState: vi.fn(),
      };
      const client = new PromptClient({ getChannel: () => channel } as any);

      await expect(client.warmup()).resolves.toBe(true);
      expect(channel.getConnectivityState).toHaveBeenCalledWith(true);
      expect(channel.watchConnectivityState).not.toHaveBeenCalled();
    });

    it("waits for READY via watchConnectivityState then resolves true", async () => {
      const states = [1, 2]; // CONNECTING on first read, READY after watch fires
      const channel = {
        getConnectivityState: vi.fn(() => states.shift()),
        watchConnectivityState: vi.fn(
          (_state: number, _deadline: Date, cb: (err?: Error) => void) => {
            cb();
          },
        ),
      };
      const client = new PromptClient({ getChannel: () => channel } as any);

      await expect(client.warmup()).resolves.toBe(true);
      expect(channel.watchConnectivityState).toHaveBeenCalledTimes(1);
    });

    it("resolves false when watchConnectivityState times out", async () => {
      const channel = {
        getConnectivityState: vi.fn(() => 1), // CONNECTING forever
        watchConnectivityState: vi.fn(
          (_state: number, _deadline: Date, cb: (err?: Error) => void) => {
            cb(new Error("Deadline exceeded"));
          },
        ),
      };
      const client = new PromptClient({ getChannel: () => channel } as any);

      await expect(client.warmup()).resolves.toBe(false);
    });
  });
});
