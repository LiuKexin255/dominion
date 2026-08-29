import { describe, expect, it, vi } from "vitest";
import * as grpc from "@grpc/grpc-js";
import { buildConversationHandlers, parseSessionResource, PROTO_PATH } from "./server.js";
import type { ConversationSink } from "./server.js";
import type { ConversationServiceHandlers } from "../agent_v2_types/projects/game/v2/ConversationService.js";
import type { ChatEvent } from "../agent_v2_types/projects/game/v2/ChatEvent.js";

/**
 * Handler-level unit tests for the gRPC status mapping
 * (specs/049-agent-v2-dsh-init/contracts/conversation-api.md §2): malformed
 * resource names and empty text are request-level INVALID_ARGUMENT failures
 * (the stream never opens), ListHistory wraps the history snapshot, and
 * Dispose is idempotent. The session sink is a `vi.fn()` double injected
 * through the buildConversationHandlers seam — no server binding, no module
 * interception (style/javascript.md Mock convention).
 */

type SendCall = Parameters<ConversationServiceHandlers["Send"]>[0];
type UnaryCall = Parameters<ConversationServiceHandlers["ListHistory"]>[0];
type UnaryCallback = Parameters<ConversationServiceHandlers["ListHistory"]>[1];

const VALID = "templates/saolei/sessions/s1";

function fakeSink(): ConversationSink & {
  send: ReturnType<typeof vi.fn>;
  listHistory: ReturnType<typeof vi.fn>;
  dispose: ReturnType<typeof vi.fn>;
} {
  return {
    send: vi.fn(),
    listHistory: vi.fn(async () => []),
    dispose: vi.fn(async () => {}),
  };
}

function fakeSendCall(request: { session?: string; text?: string }) {
  const written: ChatEvent[] = [];
  const errors: grpc.ServiceError[] = [];
  const errorListeners: Array<(err: Error) => void> = [];
  const call = {
    request,
    write: vi.fn((event: ChatEvent) => {
      written.push(event);
    }),
    end: vi.fn(),
    emit: vi.fn((name: string, err: grpc.ServiceError) => {
      if (name === "error") {
        errors.push(err);
        for (const listener of errorListeners) {
          listener(err);
        }
      }
    }),
    // The Send handler registers an 'error' listener on the real call
    // (long-lived stream write guard); the double records listeners so tests
    // can drive the failure path through emit().
    on: vi.fn((name: string, listener: (err: Error) => void) => {
      if (name === "error") {
        errorListeners.push(listener);
      }
      return call;
    }),
  };
  return { call: call as unknown as SendCall, written, end: call.end, errors };
}

function invokeSend(handlers: ConversationServiceHandlers, request: { session?: string; text?: string }) {
  const fake = fakeSendCall(request);
  handlers.Send(fake.call);
  return fake;
}

function invokeUnary(
  handler: (call: UnaryCall, callback: UnaryCallback) => void,
  request: { session?: string },
): ReturnType<typeof vi.fn> {
  const callback = vi.fn();
  handler({ request } as unknown as UnaryCall, callback as unknown as UnaryCallback);
  return callback;
}

describe("parseSessionResource", () => {
  it("resolves the runtime proto at its canonical import path under the service root", () => {
    // runtime_protos materializes the app-root proto at its standard import
    // path (tools/release/deploy/README.md §runtime_protos); the demo loads
    // its app-root proto the same way.
    expect(PROTO_PATH.endsWith("projects/game/agent_v2.proto")).toBe(true);
  });

  it("accepts the game session resource shape with a known template", () => {
    expect(parseSessionResource(VALID)).toEqual({ template: "saolei", session: "s1" });
  });

  it("rejects malformed names, unknown templates, and empty segments", () => {
    expect(parseSessionResource("templates/t/sessions/s")).toBeUndefined();
    expect(parseSessionResource("templates//sessions/s1")).toBeUndefined();
    expect(parseSessionResource("templates/saolei/sessions/")).toBeUndefined();
    expect(parseSessionResource("templates/saolei/sessions/s1/extra")).toBeUndefined();
    expect(parseSessionResource("conversations/s1")).toBeUndefined();
    expect(parseSessionResource("")).toBeUndefined();
  });
});

describe("ConversationService.Send handler", () => {
  it("adapts the grpc call into the TurnStream and dispatches to the sink", () => {
    const sink = fakeSink();
    const handlers = buildConversationHandlers(sink);
    const fake = invokeSend(handlers, { session: VALID, text: "hello" });

    expect(sink.send).toHaveBeenCalledTimes(1);
    const [session, text, stream] = (sink.send as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(session).toBe(VALID);
    expect(text).toBe("hello");

    const event = { session: VALID, turnId: "t1", turnStart: {} } as unknown as ChatEvent;
    stream.write(event);
    expect(fake.written).toEqual([event]);
    stream.end();
    expect(fake.end).toHaveBeenCalled();
  });

  it("rejects a malformed session resource before the stream opens", () => {
    const sink = fakeSink();
    const handlers = buildConversationHandlers(sink);
    const fake = invokeSend(handlers, { session: "projects/p1", text: "hello" });

    expect(fake.errors).toHaveLength(1);
    expect(fake.errors[0]?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(fake.errors[0]?.details).toContain("templates/{template}/sessions/{session}");
    expect(sink.send).not.toHaveBeenCalled();
  });

  it("rejects an unknown template segment", () => {
    const sink = fakeSink();
    const handlers = buildConversationHandlers(sink);
    const fake = invokeSend(handlers, { session: "templates/unknown/sessions/s1", text: "hello" });

    expect(fake.errors).toHaveLength(1);
    expect(fake.errors[0]?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(sink.send).not.toHaveBeenCalled();
  });

  it("rejects an empty text with INVALID_ARGUMENT", () => {
    const sink = fakeSink();
    const handlers = buildConversationHandlers(sink);
    const fake = invokeSend(handlers, { session: VALID, text: "" });

    expect(fake.errors).toHaveLength(1);
    expect(fake.errors[0]?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(fake.errors[0]?.details).toContain("non-empty");
    expect(sink.send).not.toHaveBeenCalled();
  });
});

describe("ConversationService.ListHistory handler", () => {
  it("wraps the history snapshot in the response message", async () => {
    const sink = fakeSink();
    const messages = [{ messageId: "m1", role: "ROLE_USER" }];
    sink.listHistory.mockResolvedValue(messages);
    const handlers = buildConversationHandlers(sink);
    const callback = invokeUnary(handlers.ListHistory, { session: VALID });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    expect(sink.listHistory).toHaveBeenCalledWith(VALID);
    expect(callback).toHaveBeenCalledWith(null, { messages });
  });

  it("rejects a malformed resource name with INVALID_ARGUMENT", () => {
    const sink = fakeSink();
    const handlers = buildConversationHandlers(sink);
    const callback = invokeUnary(handlers.ListHistory, { session: "nope" });

    expect(callback).toHaveBeenCalledTimes(1);
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(sink.listHistory).not.toHaveBeenCalled();
  });

  it("maps a sink failure to INTERNAL without throwing", async () => {
    const sink = fakeSink();
    sink.listHistory.mockRejectedValue(new Error("boom"));
    const handlers = buildConversationHandlers(sink);
    const callback = invokeUnary(handlers.ListHistory, { session: VALID });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.INTERNAL);
    expect(error?.message).toContain("boom");
  });
});

describe("ConversationService.Dispose handler", () => {
  it("acknowledges with an Empty reply (idempotent)", async () => {
    const sink = fakeSink();
    const handlers = buildConversationHandlers(sink);
    const callback = invokeUnary(handlers.Dispose, { session: VALID });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    expect(sink.dispose).toHaveBeenCalledWith(VALID);
    expect(callback).toHaveBeenCalledWith(null, {});
  });

  it("rejects a malformed resource name with INVALID_ARGUMENT", () => {
    const sink = fakeSink();
    const handlers = buildConversationHandlers(sink);
    const callback = invokeUnary(handlers.Dispose, { session: "templates/unknown/sessions/s1" });

    expect(callback).toHaveBeenCalledTimes(1);
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(sink.dispose).not.toHaveBeenCalled();
  });

  it("maps a dispose failure to INTERNAL", async () => {
    const sink = fakeSink();
    sink.dispose.mockRejectedValue(new Error("handle gone"));
    const handlers = buildConversationHandlers(sink);
    const callback = invokeUnary(handlers.Dispose, { session: VALID });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.INTERNAL);
    expect(error?.message).toContain("handle gone");
  });
});
