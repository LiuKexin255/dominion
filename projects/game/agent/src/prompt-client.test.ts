/**
 * Tests for PromptClient.
 *
 * Reliable pattern (FR-009): the PromptClient ctor accepts an injected gRPC
 * client (DI seam), so getTeamProfile/close/warmup tests pass a `vi.fn()`-backed
 * client and need NO module-level `vi.mock`. The channel-option tests use the
 * exported `buildChannelOptionsForTest()` factory seam.
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
    getTeamProfile: ReturnType<typeof vi.fn>;
    close: ReturnType<typeof vi.fn>;
  };

  beforeEach(() => {
    mockClient = {
      getTeamProfile: vi.fn(),
      close: vi.fn(),
    };
  });

  describe("getTeamProfile", () => {
    // The real gRPC stub contract (prompt-client.ts getTeamProfile) is
    // getTeamProfile(request, metadata, options, callback) with request
    // { name: "templates/<template>/profiles/<profile>" }. Mocks must match
    // that 4-arg shape so the 4th positional arg is the callback.
    it("returns playerModel and plannerModel from the saolei oneof variant", async () => {
      const template = "saolei";
      const profileName = "my-profile";
      const expectedPlayerModel = "opencode-go/deepseek-v4-pro";
      const expectedPlannerModel = "opencode-go/deepseek-v4";

      mockClient.getTeamProfile.mockImplementation(
        (
          _req: { name: string },
          _metadata: unknown,
          _options: { deadline: Date },
          cb: (err: null, response: { spec: string; saolei: { playerModel: string; plannerModel: string } }) => void,
        ) => {
          cb(null, {
            spec: "saolei",
            saolei: {
              playerModel: expectedPlayerModel,
              plannerModel: expectedPlannerModel,
            },
          });
        },
      );

      const client = new PromptClient(mockClient as any);
      const result = await client.getTeamProfile(template, profileName);

      expect(result).toEqual({
        playerModel: expectedPlayerModel,
        plannerModel: expectedPlannerModel,
      });
      expect(mockClient.getTeamProfile).toHaveBeenCalledTimes(1);
      expect(mockClient.getTeamProfile).toHaveBeenCalledWith(
        { name: `templates/${template}/profiles/${profileName}` },
        expect.any(Object),
        expect.any(Object),
        expect.any(Function),
      );
    });

    it("throws with NOT_FOUND for a missing profile", async () => {
      const notFoundError = Object.assign(
        new Error("Team profile not found"),
        { code: 5 }, // grpc.status.NOT_FOUND
      );

      mockClient.getTeamProfile.mockImplementation(
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

      await expect(client.getTeamProfile("saolei", "nonexistent")).rejects.toThrow(
        "Team profile not found",
      );
    });

    it("propagates non-NOT_FOUND gRPC errors", async () => {
      const error = Object.assign(
        new Error("Service unavailable"),
        { code: 14 }, // grpc.status.UNAVAILABLE
      );

      mockClient.getTeamProfile.mockImplementation(
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

      await expect(client.getTeamProfile("saolei", "error-profile")).rejects.toThrow(
        "Service unavailable",
      );
    });

    it("throws when the oneof spec is unset (contract violation, no silent fallback)", async () => {
      mockClient.getTeamProfile.mockImplementation(
        (
          _req: { name: string },
          _metadata: unknown,
          _options: { deadline: Date },
          cb: (err: null, response: { spec?: string }) => void,
        ) => {
          cb(null, {});
        },
      );

      const client = new PromptClient(mockClient as any);

      await expect(client.getTeamProfile("saolei", "no-spec")).rejects.toThrow(
        /oneof spec must be saolei/,
      );
    });

    it("throws when the oneof spec is a non-saolei variant (saolei is the only template)", async () => {
      mockClient.getTeamProfile.mockImplementation(
        (
          _req: { name: string },
          _metadata: unknown,
          _options: { deadline: Date },
          cb: (err: null, response: { spec: string; other?: unknown }) => void,
        ) => {
          cb(null, { spec: "other" });
        },
      );

      const client = new PromptClient(mockClient as any);

      await expect(client.getTeamProfile("saolei", "other-spec")).rejects.toThrow(
        /oneof spec must be saolei/,
      );
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
