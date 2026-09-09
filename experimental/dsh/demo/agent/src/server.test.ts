import { describe, expect, it, vi } from "vitest";
import * as grpc from "@grpc/grpc-js";
import { PresetAuthoringError } from "@dominion/dsh-preset-authoring";
import { buildChatHandlers, conversationIdOf } from "./server.js";
import type { ChatSessionSink } from "./server.js";
import { ConversationNotCreatedError } from "./session.js";
import type { ChatHandlers } from "../chat_types/experimental/dsh/demo/Chat.js";

/**
 * Handler-level unit tests for the gRPC status mapping
 * (specs/047-dsh-chat-demo/contracts/chat-api.md §1 error table and
 * specs/058-dsh-preset-roster-demo/contracts/chat-api.md §1.1): malformed
 * requests map to INVALID_ARGUMENT, the not-created domain error to
 * FAILED_PRECONDITION (FR-002), preset-authoring rejections to their codes
 * one-to-one, agent round failures to INTERNAL — and none of them ever take
 * the process down (fake-llm unreachable edge case).
 *
 * The session sink is a `vi.fn()` double injected through the
 * buildChatHandlers seam — no server binding, no module interception
 * (style/javascript.md Mock convention).
 */

type SendMessageCall = Parameters<ChatHandlers["SendMessage"]>[0];
type SendMessageCallback = Parameters<ChatHandlers["SendMessage"]>[1];
type CreateConversationCall = Parameters<ChatHandlers["CreateConversation"]>[0];
type CreateConversationCallback = Parameters<ChatHandlers["CreateConversation"]>[1];

function fakeSink(): ChatSessionSink {
  return {
    create: vi.fn(async (conversationId: string, preset?: string) => ({
      name: `conversations/${conversationId}`,
      preset: preset ?? "demo-standard",
      createTime: new Date(0),
    })),
    send: vi.fn(async () => ""),
  };
}

function invokeSend(
  sink: ChatSessionSink,
  request: { name?: string; message?: string },
): ReturnType<typeof vi.fn> {
  const callback = vi.fn();
  buildChatHandlers(sink).SendMessage(
    { request } as unknown as SendMessageCall,
    callback as unknown as SendMessageCallback,
  );
  return callback;
}

function invokeCreate(
  sink: ChatSessionSink,
  request: { conversationId?: string; preset?: string },
): ReturnType<typeof vi.fn> {
  const callback = vi.fn();
  buildChatHandlers(sink).CreateConversation(
    { request } as unknown as CreateConversationCall,
    callback as unknown as CreateConversationCallback,
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

describe("Chat.CreateConversation handler", () => {
  it("returns the conversation resource view with the resolved preset", async () => {
    const sink = fakeSink();
    const callback = invokeCreate(sink, { conversationId: "conv-1", preset: "demo-tools" });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    expect(sink.create).toHaveBeenCalledWith("conv-1", "demo-tools");
    const [err, view] = callback.mock.calls[0] as [null, Record<string, unknown>];
    expect(err).toBeNull();
    expect(view.name).toBe("conversations/conv-1");
    expect(view.preset).toBe("demo-tools");
    // 1970-01-01T00:00:00Z from the fake sink's fixed createTime.
    expect(view.createTime).toEqual({ seconds: 0, nanos: 0 });
  });

  it("passes an absent preset through as undefined (roster default)", async () => {
    const sink = fakeSink();
    const callback = invokeCreate(sink, { conversationId: "conv-1" });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    expect(sink.create).toHaveBeenCalledWith("conv-1", undefined);
  });

  it("rejects a malformed conversation id (INVALID_ARGUMENT)", () => {
    const sink = fakeSink();

    const tests = [
      { name: "empty id", conversationId: "" },
      { name: "uppercase", conversationId: "Conv-1" },
      { name: "underscore", conversationId: "conv_1" },
      { name: "path traversal", conversationId: "../escape" },
      { name: "leading hyphen", conversationId: "-conv" },
    ];

    for (const tt of tests) {
      const callback = invokeCreate(sink, { conversationId: tt.conversationId });
      const error = callback.mock.calls[0][0] as grpc.ServiceError;
      expect(error?.code, tt.name).toBe(grpc.status.INVALID_ARGUMENT);
      expect(sink.create, tt.name).not.toHaveBeenCalled();
    }
  });

  it("maps a preset-authoring rejection onto its error code with the message passed through", async () => {
    // The codes map one-to-one onto gRPC statuses
    // (preset-authoring-plugin.md §6): an unknown preset surfaces as
    // INVALID_ARGUMENT carrying the roster's available-ids message.
    const tests = [
      {
        name: "unknown preset",
        code: "INVALID_ARGUMENT",
        message: 'unknown preset "nope"; available: demo-standard, demo-tools',
        want: grpc.status.INVALID_ARGUMENT,
      },
      {
        name: "broken preset",
        code: "INVALID_ARGUMENT",
        message: 'preset "bad" is broken: unparsable composition',
        want: grpc.status.INVALID_ARGUMENT,
      },
      {
        name: "internal failure",
        code: "INTERNAL",
        message: "roster operation failed: disk gone",
        want: grpc.status.INTERNAL,
      },
    ];

    for (const tt of tests) {
      const sink = fakeSink();
      sink.create = vi.fn(async () => {
        throw new PresetAuthoringError(
          tt.code as "INVALID_ARGUMENT" | "INTERNAL",
          tt.message,
        );
      });
      const callback = invokeCreate(sink, { conversationId: "conv-1", preset: "nope" });

      await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
      const error = callback.mock.calls[0][0] as grpc.ServiceError;
      expect(error?.code, tt.name).toBe(tt.want);
      expect(error?.message, tt.name).toContain(tt.message);
    }
  });

  it("maps an unexpected create failure to INTERNAL without throwing", async () => {
    const sink = fakeSink();
    sink.create = vi.fn(async () => {
      throw new Error("agents factory exploded");
    });
    const callback = invokeCreate(sink, { conversationId: "conv-1" });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.INTERNAL);
    expect(error?.message).toContain("agents factory exploded");
  });
});

describe("Chat.SendMessage handler", () => {
  it("maps a successful round to the echo response", async () => {
    const sink = fakeSink();
    sink.send = vi.fn(async () => "Hello!");
    const callback = invokeSend(sink, {
      name: "conversations/conv-1",
      message: "hello there",
    });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    expect(sink.send).toHaveBeenCalledWith("conv-1", "hello there");
    expect(callback).toHaveBeenCalledWith(null, {
      name: "conversations/conv-1",
      reply: "Hello!",
    });
  });

  it("rejects a name that is not a conversations/* resource (INVALID_ARGUMENT)", () => {
    const sink = fakeSink();
    const callback = invokeSend(sink, {
      name: "projects/p1",
      message: "hello",
    });

    expect(callback).toHaveBeenCalledTimes(1);
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(error?.message).toContain("conversations/{id}");
    expect(sink.send).not.toHaveBeenCalled();
  });

  it("rejects an empty conversation id (INVALID_ARGUMENT)", () => {
    const sink = fakeSink();
    const callback = invokeSend(sink, {
      name: "conversations/",
      message: "hello",
    });

    expect(callback).toHaveBeenCalledTimes(1);
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(sink.send).not.toHaveBeenCalled();
  });

  it("rejects an empty message (INVALID_ARGUMENT)", () => {
    const sink = fakeSink();
    const callback = invokeSend(sink, {
      name: "conversations/conv-1",
      message: "",
    });

    expect(callback).toHaveBeenCalledTimes(1);
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(error?.message).toContain("message");
    expect(sink.send).not.toHaveBeenCalled();
  });

  it("maps a not-created conversation to FAILED_PRECONDITION (FR-002)", async () => {
    // No lazy creation: a send on a conversation that was never created is
    // the domain error with its actionable message passed through
    // (specs/058-dsh-preset-roster-demo/contracts/chat-api.md §1.2).
    const sink = fakeSink();
    sink.send = vi.fn(async () => {
      throw new ConversationNotCreatedError("conv-1");
    });
    const callback = invokeSend(sink, {
      name: "conversations/conv-1",
      message: "hello",
    });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.FAILED_PRECONDITION);
    expect(error?.message).toContain("call CreateConversation first");
  });

  it("maps an agent round failure to INTERNAL without throwing", async () => {
    // fake-llm unreachable edge case: the request fails with INTERNAL and
    // the handler returns normally — the serving process stays alive.
    const sink = fakeSink();
    sink.send = vi.fn(async () => {
      throw new Error("fake-llm unreachable (TRANSPORT)");
    });
    const callback = invokeSend(sink, {
      name: "conversations/conv-1",
      message: "hello",
    });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.INTERNAL);
    expect(error?.message).toContain("fake-llm unreachable");
  });
});
