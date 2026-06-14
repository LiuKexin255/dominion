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
vi.mock("@grpc/grpc-js", () => {
  // A simple constructor that accepts (target, credentials) and returns
  // an object with a mockable getAgentProfile method.
  const MockClient = vi.fn(() => ({
    getAgentProfile: vi.fn(),
    close: vi.fn(),
  }));

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
  });

  describe("getProfile", () => {
    it("returns model and systemPrompt for a valid profile", async () => {
      const profileName = "my-profile";
      const expectedModel = "opencode-go/deepseek-v4-pro";
      const expectedSystemPrompt = "You are a helpful assistant.";

      mockClient.getAgentProfile.mockImplementation(
        (
          _req: { agentProfileName: string },
          cb: (err: null, response: { model: string; systemPrompt: string }) => void,
        ) => {
          cb(null, {
            model: expectedModel,
            systemPrompt: expectedSystemPrompt,
          });
        },
      );

      const client = new PromptClient(mockClient as any);
      const result = await client.getProfile(profileName);

      expect(result).toEqual({
        model: expectedModel,
        systemPrompt: expectedSystemPrompt,
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
            response: { model: string; systemPrompt: string },
          ) => void,
        ) => {
          cb(null, { model, systemPrompt });
        },
      );

      const client = new PromptClient(mockClient as any);
      const result = await client.getProfile(profileName);

      // Verify exact field extraction
      expect(result.model).toBe(model);
      expect(result.systemPrompt).toBe(systemPrompt);

      // Verify the RPC was called with the correct profile name
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
  });

  describe("close", () => {
    it("closes the underlying gRPC client", () => {
      const client = new PromptClient(mockClient as any);
      client.close();
      expect(mockClient.close).toHaveBeenCalledTimes(1);
    });
  });
});
