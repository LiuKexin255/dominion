import { describe, expect, it, vi } from "vitest";
import * as grpc from "@grpc/grpc-js";
import { buildChatHandlers, conversationIdOf } from "./server.js";
import type { ChatSessionSink } from "./server.js";
import type { ChatHandlers } from "../chat_types/experimental/dsh/demo/Chat.js";

/**
 * Handler-level unit tests for the gRPC status mapping
 * (specs/047-dsh-chat-demo/contracts/chat-api.md §1 error table): malformed
 * requests map to INVALID_ARGUMENT, agent round failures to INTERNAL, and
 * neither ever takes the process down (fake-llm unreachable edge case).
 *
 * The session sink is a `vi.fn()` double injected through the
 * buildChatHandlers seam — no server binding, no module interception
 * (style/javascript.md Mock convention).
 */

type SendMessageCall = Parameters<ChatHandlers["SendMessage"]>[0];
type SendMessageCallback = Parameters<ChatHandlers["SendMessage"]>[1];

function invoke(
  handlers: ChatHandlers,
  request: { name?: string; message?: string },
): ReturnType<typeof vi.fn> {
  const callback = vi.fn();
  handlers.SendMessage(
    { request } as unknown as SendMessageCall,
    callback as unknown as SendMessageCallback,
  );
  return callback;
}

describe("conversationIdOf", () => {
  it("extracts the suffix of a conversations/{id} resource name", () => {
    expect(conversationIdOf("conversations/conv-1")).toBe("conv-1");
    expect(conversationIdOf("conversations/")).toBe("");
    expect(conversationIdOf("projects/p1")).toBe("");
    expect(conversationIdOf("")).toBe("");
  });
});

describe("Chat.SendMessage handler", () => {
  it("maps a successful round to the echo response", async () => {
    const send = vi.fn(async (_id: string, _text: string) => "Hello!");
    const callback = invoke(buildChatHandlers({ send }), {
      name: "conversations/conv-1",
      message: "hello there",
    });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    expect(send).toHaveBeenCalledWith("conv-1", "hello there");
    expect(callback).toHaveBeenCalledWith(null, {
      name: "conversations/conv-1",
      reply: "Hello!",
    });
  });

  it("rejects a name that is not a conversations/* resource (INVALID_ARGUMENT)", () => {
    const send = vi.fn(async () => "");
    const callback = invoke(buildChatHandlers({ send }), {
      name: "projects/p1",
      message: "hello",
    });

    expect(callback).toHaveBeenCalledTimes(1);
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(error?.message).toContain("conversations/{id}");
    expect(send).not.toHaveBeenCalled();
  });

  it("rejects an empty conversation id (INVALID_ARGUMENT)", () => {
    const send = vi.fn(async () => "");
    const callback = invoke(buildChatHandlers({ send }), {
      name: "conversations/",
      message: "hello",
    });

    expect(callback).toHaveBeenCalledTimes(1);
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(send).not.toHaveBeenCalled();
  });

  it("rejects an empty message (INVALID_ARGUMENT)", () => {
    const send = vi.fn(async () => "");
    const callback = invoke(buildChatHandlers({ send }), {
      name: "conversations/conv-1",
      message: "",
    });

    expect(callback).toHaveBeenCalledTimes(1);
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(error?.message).toContain("message");
    expect(send).not.toHaveBeenCalled();
  });

  it("maps an agent round failure to INTERNAL without throwing", async () => {
    // fake-llm unreachable edge case: the request fails with INTERNAL and
    // the handler returns normally — the serving process stays alive.
    const send = vi.fn(async () => {
      throw new Error("fake-llm unreachable (TRANSPORT)");
    });
    const callback = invoke(buildChatHandlers({ send }), {
      name: "conversations/conv-1",
      message: "hello",
    });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.INTERNAL);
    expect(error?.message).toContain("fake-llm unreachable");
  });
});
