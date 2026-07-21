/**
 * Tests for PromptClient.
 *
 * Reliable pattern (FR-009): the PromptClient ctor accepts an injected gRPC
 * client (DI seam), so getProfile/close/warmup tests pass a `vi.fn()`-backed
 * client and need NO module-level `vi.mock`. The channel-option tests use the
 * exported `buildChannelOptionsForTest()` factory seam. Previously this file
 * relied on module-level `vi.mock` of `@grpc/grpc-js` / `node:fs` /
 * `@grpc/proto-loader` / `@dominion/common-js-grpc-resolver`, which the
 * pre-compiled `:lib` bypasses under Bazel js_test (see research.md §2 and
 * style/javascript.md §测试). `registerDominionResolver` now runs only on the
 * real-construction path, so DI-seamed construction no longer triggers it.
 */
import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  PromptClient,
  buildChannelOptionsForTest,
} from "./prompt-client";

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
  });

  describe("getProfile", () => {
    // The real gRPC stub contract (prompt-client.ts getProfile) is
    // getAgentProfile(request, metadata, options, callback) with request
    // { name: "prompts/agentProfiles/<profile>" }. Mocks must match that
    // 4-arg shape so the 4th positional arg is the callback (FR-012).
    it("returns model and systemPrompt for a valid profile", async () => {
      const profileName = "my-profile";
      const expectedModel = "opencode-go/deepseek-v4-pro";
      const expectedSystemPrompt = "You are a helpful assistant.";

      mockClient.getAgentProfile.mockImplementation(
        (
          _req: { name: string },
          _metadata: unknown,
          _options: { deadline: Date },
          cb: (err: null, response: { model: string; systemPrompt: string; toolNames: string[]; mcpNames?: string[] }) => void,
        ) => {
          cb(null, {
            model: expectedModel,
            systemPrompt: expectedSystemPrompt,
            toolNames: ["mouse_move"],
            mcpNames: ["saolei"],
          });
        },
      );

      const client = new PromptClient(mockClient as any);
      const result = await client.getProfile(profileName);

      expect(result).toEqual({
        model: expectedModel,
        systemPrompt: expectedSystemPrompt,
        toolNames: ["mouse_move"],
        mcpNames: ["saolei"],
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
          _req: { name: string },
          _metadata: unknown,
          _options: { deadline: Date },
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
          _req: { name: string },
          _metadata: unknown,
          _options: { deadline: Date },
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
        { name: `prompts/agentProfiles/${profileName}` },
        expect.any(Object),
        expect.any(Object),
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
          _req: { name: string },
          _metadata: unknown,
          _options: { deadline: Date },
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
          _req: { name: string },
          _metadata: unknown,
          _options: { deadline: Date },
          cb: (
            err: null,
            response: { model: string; systemPrompt: string; toolNames: string[] },
          ) => void,
        ) => {
          cb(null, {
            model: "m",
            systemPrompt: "s",
            toolNames: ["mouse_move", "mouse_click"],
          });
        },
      );

      const client = new PromptClient(mockClient as any);
      const result = await client.getProfile("tools-profile");

      expect(result.toolNames).toEqual(["mouse_move", "mouse_click"]);
    });

    it("defaults toolNames to empty array when absent in response", async () => {
      mockClient.getAgentProfile.mockImplementation(
        (
          _req: { name: string },
          _metadata: unknown,
          _options: { deadline: Date },
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

    // Spec 018-saolei-mcp FR-021 / data-model.md §9: mcpNames carries the
    // profile's MCP integrations (e.g. "saolei"). The field defaults to []
    // for older profiles that omit it on the wire.
    it("extracts mcpNames from the response", async () => {
      mockClient.getAgentProfile.mockImplementation(
        (
          _req: { name: string },
          _metadata: unknown,
          _options: { deadline: Date },
          cb: (
            err: null,
            response: { model: string; systemPrompt: string; toolNames: string[]; mcpNames: string[] },
          ) => void,
        ) => {
          cb(null, {
            model: "m",
            systemPrompt: "s",
            toolNames: [],
            mcpNames: ["saolei"],
          });
        },
      );

      const client = new PromptClient(mockClient as any);
      const result = await client.getProfile("mcp-profile");

      expect(result.mcpNames).toEqual(["saolei"]);
    });

    it("defaults mcpNames to empty array when absent in response", async () => {
      mockClient.getAgentProfile.mockImplementation(
        (
          _req: { name: string },
          _metadata: unknown,
          _options: { deadline: Date },
          cb: (
            err: null,
            response: { model: string; systemPrompt: string; toolNames?: string[] },
          ) => void,
        ) => {
          cb(null, { model: "m", systemPrompt: "s" });
        },
      );

      const client = new PromptClient(mockClient as any);
      const result = await client.getProfile("no-mcp");

      expect(result.mcpNames).toEqual([]);
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
    // Factory seam (FR-009): assert channel options directly via the exported
    // builder instead of intercepting the grpc.Client constructor with a
    // module-level vi.mock.
    it("configures keepalive and reconnect-backoff channel options", () => {
      const options = buildChannelOptionsForTest();
      // KEEPALIVE_OPTIONS in prompt-client.ts deliberately sets a 5-min
      // keepalive interval (grpc-go rejects pings more frequent than 5 min)
      // and disables ping-without-calls. The test asserts that documented
      // intent, not a stale 30s/1 value.
      expect(options?.["grpc.keepalive_time_ms"]).toBe(300_000);
      expect(options?.["grpc.keepalive_timeout_ms"]).toBe(10_000);
      expect(options?.["grpc.keepalive_permit_without_calls"]).toBe(0);
      expect(options?.["grpc.initial_reconnect_backoff_ms"]).toBe(1_000);
      expect(options?.["grpc.max_reconnect_backoff_ms"]).toBe(15_000);
    });

    it("configures round_robin load balancing via grpc.service_config", () => {
      const options = buildChannelOptionsForTest();
      const serviceConfig = JSON.parse(
        options?.["grpc.service_config"] as string,
      );
      expect(serviceConfig.loadBalancingConfig).toEqual([
        { round_robin: {} },
      ]);
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
