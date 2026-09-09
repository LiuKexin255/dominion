import { describe, expect, it, vi } from "vitest";
import * as grpc from "@grpc/grpc-js";
import { PresetAuthoringError } from "@dominion/dsh-preset-authoring";
import type { PresetAuthoringService, PresetView } from "@dominion/dsh-preset-authoring";
import { buildChatHandlers, buildPresetHandlers, conversationIdOf, presetIdOf } from "./server.js";
import type { ChatSessionSink } from "./server.js";
import { ConversationNotCreatedError } from "./session.js";
import type { ChatHandlers } from "../chat_types/experimental/dsh/demo/Chat.js";
import type { PresetServiceHandlers } from "../chat_types/experimental/dsh/demo/PresetService.js";

/**
 * Handler-level unit tests for the gRPC status mapping
 * (specs/047-dsh-chat-demo/contracts/chat-api.md §1 error table and
 * specs/058-dsh-preset-roster-demo/contracts/chat-api.md §1/§2): malformed
 * requests map to INVALID_ARGUMENT, the not-created domain error to
 * FAILED_PRECONDITION (FR-002), preset-authoring rejections to their codes
 * one-to-one (including the template-delete refusal → FAILED_PRECONDITION),
 * agent round failures to INTERNAL — and none of them ever take the process
 * down (fake-llm unreachable edge case).
 *
 * The session sink and the preset-authoring service are `vi.fn()` doubles
 * injected through the buildChatHandlers/buildPresetHandlers seams — no
 * server binding, no module interception (style/javascript.md Mock
 * convention).
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

/** The authored-preset view the fake authoring service returns. */
function authoredView(id: string, persona = "You are the authored assistant."): PresetView {
  return {
    id,
    template: "demo-tools",
    persona,
    displayName: undefined,
    createTime: new Date(0),
    updateTime: new Date(0),
  };
}

function fakeAuthoring(): PresetAuthoringService {
  return {
    compose: vi.fn(),
    create: vi.fn(async (input: { id: string }) => authoredView(input.id)),
    get: vi.fn(async (id: string) => authoredView(id)),
    list: vi.fn(async () => [authoredView("authored-a"), authoredView("authored-b")]),
    update: vi.fn(async (id: string) => authoredView(id)),
    remove: vi.fn(async () => undefined),
  };
}

function invokePreset<K extends keyof PresetServiceHandlers>(
  authoring: PresetAuthoringService,
  method: K,
  request: unknown,
): ReturnType<typeof vi.fn> {
  const callback = vi.fn();
  (buildPresetHandlers(authoring)[method] as (call: unknown, cb: unknown) => void)(
    { request },
    callback,
  );
  return callback;
}

describe("conversationIdOf", () => {
  it("extracts the suffix of a well-formed conversations/{id} resource name", () => {
    expect(conversationIdOf("conversations/conv-1")).toBe("conv-1");
    expect(conversationIdOf("conversations/")).toBe("");
    expect(conversationIdOf("projects/p1")).toBe("");
    expect(conversationIdOf("")).toBe("");
  });

  it("rejects a resource name whose id violates the conversation id grammar", () => {
    // A malformed resource name must surface as INVALID_ARGUMENT at the
    // handler, never as a NOT_FOUND lookup (AIP-122) — so the extractor
    // refuses ids the grammar cannot accept.
    expect(conversationIdOf("conversations/conv/1")).toBe("");
    expect(conversationIdOf("conversations/Conv-1")).toBe("");
    expect(conversationIdOf("conversations/conv_1")).toBe("");
  });
});

describe("presetIdOf", () => {
  it("extracts the suffix of a well-formed presets/{id} resource name", () => {
    expect(presetIdOf("presets/authored-1")).toBe("authored-1");
    expect(presetIdOf("presets/")).toBe("");
    expect(presetIdOf("conversations/conv-1")).toBe("");
    expect(presetIdOf("")).toBe("");
  });

  it("rejects a resource name whose id violates the preset id grammar", () => {
    // Symmetric with CreatePreset's preset_id check: `presets/foo/bar` must
    // fail INVALID_ARGUMENT at the handler, never fall through to a store
    // NOT_FOUND (AIP-122).
    expect(presetIdOf("presets/foo/bar")).toBe("");
    expect(presetIdOf("presets/Authored-1")).toBe("");
    expect(presetIdOf("presets/authored_1")).toBe("");
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

  it("rejects a name that is not a well-formed conversations/{id} resource (INVALID_ARGUMENT)", () => {
    const tests = [
      { name: "wrong collection", resource: "projects/p1" },
      { name: "multi-segment id", resource: "conversations/conv/1" },
    ];

    for (const tt of tests) {
      const sink = fakeSink();
      const callback = invokeSend(sink, { name: tt.resource, message: "hello" });
      const error = callback.mock.calls[0][0] as grpc.ServiceError;
      expect(error?.code, tt.name).toBe(grpc.status.INVALID_ARGUMENT);
      expect(error?.message, tt.name).toContain("conversations/{id}");
      expect(sink.send, tt.name).not.toHaveBeenCalled();
    }
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

describe("PresetService.CreatePreset handler", () => {
  it("returns the Preset resource view of the authored copy", async () => {
    const authoring = fakeAuthoring();
    const callback = invokePreset(authoring, "CreatePreset", {
      presetId: "authored-1",
      template: "demo-tools",
      persona: "You are the authored assistant.",
    });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    expect(authoring.create).toHaveBeenCalledWith({
      id: "authored-1",
      template: "demo-tools",
      persona: "You are the authored assistant.",
      displayName: undefined,
    });
    const [err, view] = callback.mock.calls[0] as [null, Record<string, unknown>];
    expect(err).toBeNull();
    expect(view.name).toBe("presets/authored-1");
    expect(view.template).toBe("demo-tools");
    expect(view.persona).toBe("You are the authored assistant.");
    expect(view.displayName).toBe("");
    expect(view.createTime).toEqual({ seconds: 0, nanos: 0 });
  });

  it("passes an explicit display_name through to the authoring service", async () => {
    const authoring = fakeAuthoring();
    const callback = invokePreset(authoring, "CreatePreset", {
      presetId: "authored-1",
      template: "demo-standard",
      persona: "p",
      displayName: "Authored One",
    });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    expect(authoring.create).toHaveBeenCalledWith({
      id: "authored-1",
      template: "demo-standard",
      persona: "p",
      displayName: "Authored One",
    });
  });

  it("normalizes an empty display_name to undefined (absent = fall back to the id)", async () => {
    // proto3 cannot distinguish an absent string from an empty one on the
    // wire (proto-loader defaults fill in ""); the handler normalizes the
    // empty wire value to undefined so the store never persists an empty
    // display name and the roster's fall-back-to-id surface applies.
    const authoring = fakeAuthoring();
    const callback = invokePreset(authoring, "CreatePreset", {
      presetId: "authored-1",
      template: "demo-tools",
      persona: "p",
      displayName: "",
    });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    expect(authoring.create).toHaveBeenCalledWith({
      id: "authored-1",
      template: "demo-tools",
      persona: "p",
      displayName: undefined,
    });
  });

  it("rejects malformed field constraints before any authoring call (INVALID_ARGUMENT)", () => {
    const tests = [
      { name: "empty id", request: { presetId: "", template: "demo-tools", persona: "p" } },
      { name: "uppercase id", request: { presetId: "Authored-1", template: "demo-tools", persona: "p" } },
      { name: "underscore id", request: { presetId: "authored_1", template: "demo-tools", persona: "p" } },
      { name: "path traversal id", request: { presetId: "../escape", template: "demo-tools", persona: "p" } },
      { name: "leading hyphen id", request: { presetId: "-authored", template: "demo-tools", persona: "p" } },
      { name: "empty template", request: { presetId: "authored-1", template: "", persona: "p" } },
      { name: "empty persona", request: { presetId: "authored-1", template: "demo-tools", persona: "" } },
      { name: "missing persona", request: { presetId: "authored-1", template: "demo-tools" } },
    ];

    for (const tt of tests) {
      const authoring = fakeAuthoring();
      const callback = invokePreset(authoring, "CreatePreset", tt.request);
      const error = callback.mock.calls[0][0] as grpc.ServiceError;
      expect(error?.code, tt.name).toBe(grpc.status.INVALID_ARGUMENT);
      expect(authoring.create, tt.name).not.toHaveBeenCalled();
    }
  });

  it("maps a duplicate id onto ALREADY_EXISTS with the message passed through", async () => {
    const authoring = fakeAuthoring();
    authoring.create = vi.fn(async () => {
      throw new PresetAuthoringError("ALREADY_EXISTS", 'preset "authored-1" already exists');
    });
    const callback = invokePreset(authoring, "CreatePreset", {
      presetId: "authored-1",
      template: "demo-tools",
      persona: "p",
    });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.ALREADY_EXISTS);
    expect(error?.message).toContain("already exists");
  });

  it("maps an unresolvable template onto INVALID_ARGUMENT carrying the available ids", async () => {
    // The roster resolve rejection surfaces verbatim
    // (preset-authoring-plugin.md §6; R5 — the available-ids message comes
    // from the roster, no ListTemplates RPC).
    const authoring = fakeAuthoring();
    authoring.create = vi.fn(async () => {
      throw new PresetAuthoringError(
        "INVALID_ARGUMENT",
        'unknown preset "no-such-template"; available: demo-standard, demo-tools',
      );
    });
    const callback = invokePreset(authoring, "CreatePreset", {
      presetId: "authored-1",
      template: "no-such-template",
      persona: "p",
    });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(error?.message).toContain("available: demo-standard, demo-tools");
  });
});

describe("PresetService.GetPreset handler", () => {
  it("returns the Preset resource view", async () => {
    const authoring = fakeAuthoring();
    const callback = invokePreset(authoring, "GetPreset", { name: "presets/authored-1" });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    expect(authoring.get).toHaveBeenCalledWith("authored-1");
    const [err, view] = callback.mock.calls[0] as [null, Record<string, unknown>];
    expect(err).toBeNull();
    expect(view.name).toBe("presets/authored-1");
  });

  it("rejects a name that is not a well-formed presets/{id} resource (INVALID_ARGUMENT)", () => {
    const tests = [
      { name: "wrong collection", resource: "conversations/conv-1" },
      { name: "multi-segment id", resource: "presets/foo/bar" },
    ];

    for (const tt of tests) {
      const authoring = fakeAuthoring();
      const callback = invokePreset(authoring, "GetPreset", { name: tt.resource });
      const error = callback.mock.calls[0][0] as grpc.ServiceError;
      expect(error?.code, tt.name).toBe(grpc.status.INVALID_ARGUMENT);
      expect(error?.message, tt.name).toContain("presets/{id}");
      expect(authoring.get, tt.name).not.toHaveBeenCalled();
    }
  });

  it("maps an unknown id onto NOT_FOUND", async () => {
    const authoring = fakeAuthoring();
    authoring.get = vi.fn(async () => {
      throw new PresetAuthoringError("NOT_FOUND", "preset ghost not found");
    });
    const callback = invokePreset(authoring, "GetPreset", { name: "presets/ghost" });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.NOT_FOUND);
  });
});

describe("PresetService.ListPresets handler", () => {
  it("returns every authored preset view", async () => {
    const authoring = fakeAuthoring();
    const callback = invokePreset(authoring, "ListPresets", {});

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    const [err, response] = callback.mock.calls[0] as [null, { presets: Record<string, unknown>[] }];
    expect(err).toBeNull();
    expect(response.presets.map((preset) => preset.name)).toEqual([
      "presets/authored-a",
      "presets/authored-b",
    ]);
  });
});

describe("PresetService.UpdatePreset handler", () => {
  it("applies only the masked fields to the authoring patch", async () => {
    const authoring = fakeAuthoring();
    const callback = invokePreset(authoring, "UpdatePreset", {
      name: "presets/authored-1",
      updateMask: { paths: ["persona"] },
      persona: "You are the rewritten persona.",
    });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    expect(authoring.update).toHaveBeenCalledWith("authored-1", {
      persona: "You are the rewritten persona.",
    });
    const [err, view] = callback.mock.calls[0] as [null, Record<string, unknown>];
    expect(err).toBeNull();
    expect(view.name).toBe("presets/authored-1");
  });

  it("passes a display_name mask through to the authoring patch", async () => {
    const authoring = fakeAuthoring();
    const callback = invokePreset(authoring, "UpdatePreset", {
      name: "presets/authored-1",
      updateMask: { paths: ["display_name"] },
      displayName: "Renamed",
    });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    expect(authoring.update).toHaveBeenCalledWith("authored-1", { displayName: "Renamed" });
  });

  it("rejects an empty update_mask (INVALID_ARGUMENT)", () => {
    const authoring = fakeAuthoring();
    const callback = invokePreset(authoring, "UpdatePreset", {
      name: "presets/authored-1",
      persona: "p",
    });

    expect(callback).toHaveBeenCalledTimes(1);
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(error?.message).toContain("update_mask");
    expect(authoring.update).not.toHaveBeenCalled();
  });

  it("rejects an update_mask path outside persona/display_name (INVALID_ARGUMENT)", () => {
    const authoring = fakeAuthoring();
    const callback = invokePreset(authoring, "UpdatePreset", {
      name: "presets/authored-1",
      updateMask: { paths: ["template"] },
    });

    expect(callback).toHaveBeenCalledTimes(1);
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(error?.message).toContain("template");
    expect(authoring.update).not.toHaveBeenCalled();
  });

  it("rejects a masked empty persona (INVALID_ARGUMENT)", () => {
    const authoring = fakeAuthoring();
    const callback = invokePreset(authoring, "UpdatePreset", {
      name: "presets/authored-1",
      updateMask: { paths: ["persona"] },
    });

    expect(callback).toHaveBeenCalledTimes(1);
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(authoring.update).not.toHaveBeenCalled();
  });

  it("rejects a name that is not a well-formed presets/{id} resource (INVALID_ARGUMENT)", () => {
    const authoring = fakeAuthoring();
    const callback = invokePreset(authoring, "UpdatePreset", {
      name: "presets/foo/bar",
      updateMask: { paths: ["persona"] },
      persona: "p",
    });

    expect(callback).toHaveBeenCalledTimes(1);
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(error?.message).toContain("presets/{id}");
    expect(authoring.update).not.toHaveBeenCalled();
  });

  it("maps an unknown id onto NOT_FOUND", async () => {
    const authoring = fakeAuthoring();
    authoring.update = vi.fn(async () => {
      throw new PresetAuthoringError("NOT_FOUND", "preset ghost not found");
    });
    const callback = invokePreset(authoring, "UpdatePreset", {
      name: "presets/ghost",
      updateMask: { paths: ["persona"] },
      persona: "p",
    });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.NOT_FOUND);
  });
});

describe("PresetService.DeletePreset handler", () => {
  it("deletes the authored preset and answers Empty", async () => {
    const authoring = fakeAuthoring();
    const callback = invokePreset(authoring, "DeletePreset", { name: "presets/authored-1" });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    expect(authoring.remove).toHaveBeenCalledWith("authored-1");
    const [err, response] = callback.mock.calls[0] as [null, Record<string, unknown>];
    expect(err).toBeNull();
    expect(response).toEqual({});
  });

  it("rejects a name that is not a well-formed presets/{id} resource (INVALID_ARGUMENT)", () => {
    const tests = [
      { name: "empty id", resource: "presets/" },
      { name: "multi-segment id", resource: "presets/foo/bar" },
    ];

    for (const tt of tests) {
      const authoring = fakeAuthoring();
      const callback = invokePreset(authoring, "DeletePreset", { name: tt.resource });
      const error = callback.mock.calls[0][0] as grpc.ServiceError;
      expect(error?.code, tt.name).toBe(grpc.status.INVALID_ARGUMENT);
      expect(authoring.remove, tt.name).not.toHaveBeenCalled();
    }
  });

  it("maps an unknown id onto NOT_FOUND", async () => {
    const authoring = fakeAuthoring();
    authoring.remove = vi.fn(async () => {
      throw new PresetAuthoringError("NOT_FOUND", "preset ghost not found");
    });
    const callback = invokePreset(authoring, "DeletePreset", { name: "presets/ghost" });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.NOT_FOUND);
  });

  it("passes a template delete refusal through as FAILED_PRECONDITION", async () => {
    // A template id is system-trust deployment data: the roster refuses the
    // removal and the plugin surfaces that refusal verbatim
    // (preset-authoring-plugin.md §6; chat-api.md §2 DeletePreset row).
    const authoring = fakeAuthoring();
    authoring.remove = vi.fn(async () => {
      throw new PresetAuthoringError(
        "FAILED_PRECONDITION",
        'preset "demo-standard" is not writable: does not live under the writable preset root',
      );
    });
    const callback = invokePreset(authoring, "DeletePreset", { name: "presets/demo-standard" });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    expect(authoring.remove).toHaveBeenCalledWith("demo-standard");
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.FAILED_PRECONDITION);
    expect(error?.message).toContain("not writable");
  });
});
