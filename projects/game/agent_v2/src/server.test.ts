import { describe, expect, it, vi } from "vitest";
import * as grpc from "@grpc/grpc-js";
import { buildAgentHandlers, buildDesktopBridgeHandlers, parseAgentParent, parseSessionResource, PROTO_PATH } from "./server.js";
import type { AgentSink } from "./server.js";
import type { AgentServiceHandlers } from "../agent_v2_types/projects/game/v2/AgentService.js";
import type { ChatEvent } from "../agent_v2_types/projects/game/v2/ChatEvent.js";

/**
 * Handler-level unit tests for the gRPC status mapping
 * (specs/051-agent-v2-dsh-migration/contracts/agent-api.md §2): malformed
 * resource names and empty text are request-level INVALID_ARGUMENT failures
 * (the stream never opens), ListAgentMessages wraps the history snapshot,
 * and the not-yet-wired handlers fail loudly with UNIMPLEMENTED. The session
 * sink is a `vi.fn()` double injected through the buildAgentHandlers seam —
 * no server binding, no module interception (style/javascript.md Mock
 * convention).
 */

type SendCall = Parameters<AgentServiceHandlers["Send"]>[0];
type UnaryCall = Parameters<AgentServiceHandlers["ListAgentMessages"]>[0];
type UnaryCallback = Parameters<AgentServiceHandlers["ListAgentMessages"]>[1];

const VALID = "templates/saolei/sessions/s1";
const VALID_AGENT = "templates/saolei/sessions/s1/agent";

function fakeSink(): AgentSink & {
  send: ReturnType<typeof vi.fn>;
  listMessages: ReturnType<typeof vi.fn>;
} {
  return {
    send: vi.fn(),
    listMessages: vi.fn(async () => []),
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

function invokeSend(handlers: AgentServiceHandlers, request: { session?: string; text?: string }) {
  const fake = fakeSendCall(request);
  handlers.Send(fake.call);
  return fake;
}

function invokeUnary(
  handler: (call: UnaryCall, callback: UnaryCallback) => void,
  request: Record<string, unknown>,
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

describe("parseAgentParent", () => {
  it("strips the /agent singleton segment and validates the session underneath", () => {
    expect(parseAgentParent(VALID_AGENT)).toEqual({ template: "saolei", session: "s1" });
  });

  it("rejects names without the agent segment and invalid sessions", () => {
    expect(parseAgentParent(VALID)).toBeUndefined();
    expect(parseAgentParent("templates/saolei/sessions/s1/agent/x")).toBeUndefined();
    expect(parseAgentParent("templates/unknown/sessions/s1/agent")).toBeUndefined();
    expect(parseAgentParent("")).toBeUndefined();
  });
});

describe("AgentService.Send handler", () => {
  it("adapts the grpc call into the TurnStream and dispatches to the sink", () => {
    const sink = fakeSink();
    const handlers = buildAgentHandlers(sink);
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
    const handlers = buildAgentHandlers(sink);
    const fake = invokeSend(handlers, { session: "projects/p1", text: "hello" });

    expect(fake.errors).toHaveLength(1);
    expect(fake.errors[0]?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(fake.errors[0]?.details).toContain("templates/{template}/sessions/{session}");
    expect(sink.send).not.toHaveBeenCalled();
  });

  it("rejects an unknown template segment", () => {
    const sink = fakeSink();
    const handlers = buildAgentHandlers(sink);
    const fake = invokeSend(handlers, { session: "templates/unknown/sessions/s1", text: "hello" });

    expect(fake.errors).toHaveLength(1);
    expect(fake.errors[0]?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(sink.send).not.toHaveBeenCalled();
  });

  it("rejects an empty text with INVALID_ARGUMENT", () => {
    const sink = fakeSink();
    const handlers = buildAgentHandlers(sink);
    const fake = invokeSend(handlers, { session: VALID, text: "" });

    expect(fake.errors).toHaveLength(1);
    expect(fake.errors[0]?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(fake.errors[0]?.details).toContain("non-empty");
    expect(sink.send).not.toHaveBeenCalled();
  });
});

describe("AgentService.ListAgentMessages handler", () => {
  it("wraps the history snapshot in the response message", async () => {
    const sink = fakeSink();
    const messages = [{ messageId: "m1", role: "ROLE_USER" }];
    sink.listMessages.mockResolvedValue(messages);
    const handlers = buildAgentHandlers(sink);
    const callback = invokeUnary(handlers.ListAgentMessages, { parent: VALID_AGENT });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    expect(sink.listMessages).toHaveBeenCalledWith(VALID);
    expect(callback).toHaveBeenCalledWith(null, { messages });
  });

  it("rejects a malformed parent with INVALID_ARGUMENT", () => {
    const sink = fakeSink();
    const handlers = buildAgentHandlers(sink);
    const callback = invokeUnary(handlers.ListAgentMessages, { parent: "nope" });

    expect(callback).toHaveBeenCalledTimes(1);
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(sink.listMessages).not.toHaveBeenCalled();
  });

  it("maps a sink failure to INTERNAL without throwing", async () => {
    const sink = fakeSink();
    sink.listMessages.mockRejectedValue(new Error("boom"));
    const handlers = buildAgentHandlers(sink);
    const callback = invokeUnary(handlers.ListAgentMessages, { parent: VALID_AGENT });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.INTERNAL);
    expect(error?.message).toContain("boom");
  });
});

describe("AgentService placeholder handlers", () => {
  it("fail loudly with UNIMPLEMENTED until the host wiring lands", async () => {
    // Phase-2 placeholders (specs/051-agent-v2-dsh-migration/tasks.md T014
    // replaces them): every not-yet-wired handler must answer UNIMPLEMENTED
    // rather than fabricating an empty success.
    const handlers = buildAgentHandlers(fakeSink());
    const methods: Array<keyof AgentServiceHandlers> = [
      "UpdateAgent",
      "GetAgent",
      "CreatePreset",
      "ListPresets",
      "GetPreset",
      "UpdatePreset",
      "DeletePreset",
      "ListModels",
    ];
    for (const method of methods) {
      const handler = handlers[method] as NonNullable<AgentServiceHandlers[typeof method]>;
      const callback = vi.fn();
      (handler as (call: unknown, cb: unknown) => void)({}, callback);
      await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
      const error = callback.mock.calls[0][0] as grpc.ServiceError;
      expect(error?.code, String(method)).toBe(grpc.status.UNIMPLEMENTED);
    }
  });
});

describe("DesktopBridgeService.Connect placeholder", () => {
  it("emits UNIMPLEMENTED on the duplex stream until the bridge plugin lands", () => {
    const handlers = buildDesktopBridgeHandlers();
    const errors: grpc.ServiceError[] = [];
    const call = {
      emit: vi.fn((name: string, err: grpc.ServiceError) => {
        if (name === "error") {
          errors.push(err);
        }
      }),
    };
    (handlers.Connect as (c: unknown) => void)(call);
    expect(errors).toHaveLength(1);
    expect(errors[0]?.code).toBe(grpc.status.UNIMPLEMENTED);
  });
});
