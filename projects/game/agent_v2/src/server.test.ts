import { describe, expect, it, vi } from "vitest";
import * as grpc from "@grpc/grpc-js";
import {
  buildAgentHandlers,
  buildDesktopBridgeHandlers,
  buildPresetHandlers,
  parseAgentParent,
  parsePresetResource,
  parseSessionResource,
  parseTemplateParent,
  PROTO_PATH,
} from "./server.js";
import type { ModelCatalogEntry } from "./server.js";
import { AgentSessionError } from "./session.js";
import { PresetStoreError } from "./presets.js";
import type { AgentServiceHandlers } from "../agent_v2_types/projects/game/v2/AgentService.js";
import type { ChatEvent } from "../agent_v2_types/projects/game/v2/ChatEvent.js";

/**
 * Handler-level unit tests for the gRPC status mapping
 * (specs/051-agent-v2-dsh-migration/contracts/agent-api.md §2): malformed
 * resource names and empty text are request-level INVALID_ARGUMENT failures
 * (the stream never opens), unmaterialized Sends are FAILED_PRECONDITION,
 * UpdateAgent validates fail-fast (preset → model catalog) before
 * materializing, and the PresetService surface (preset CRUD + ListModels,
 * directive-2026-09-01.md §3.7) delegates to the store/catalog with AIP
 * error mapping. The collaborators are `vi.fn()` doubles injected through
 * the buildAgentHandlers/buildPresetHandlers seams — no server binding, no
 * module interception (style/javascript.md Mock convention).
 */

type SendCall = Parameters<AgentServiceHandlers["Send"]>[0];
type UnaryCall = Parameters<AgentServiceHandlers["ListAgentMessages"]>[0];
type UnaryCallback = Parameters<AgentServiceHandlers["ListAgentMessages"]>[1];

const VALID = "templates/saolei/sessions/s1";
const VALID_AGENT = "templates/saolei/sessions/s1/agent";
const VALID_PRESET = "templates/saolei/presets/p1";

function fakeDeps() {
  return {
    sessions: {
      send: vi.fn(),
      listMessages: vi.fn(async () => []),
      materialize: vi.fn(async () => ({
        name: VALID_AGENT,
        preset: VALID_PRESET,
        model: "glm-5.2",
        createTime: new Date(1000),
        updateTime: new Date(2000),
      })),
      getAgent: vi.fn(() => ({
        name: VALID_AGENT,
        preset: VALID_PRESET,
        model: "glm-5.2",
        createTime: new Date(1000),
        updateTime: new Date(2000),
      })),
    },
    presets: {
      create: vi.fn(async () => undefined),
      get: vi.fn(async () => ({
        name: VALID_PRESET,
        playerPrompt: "play carefully",
        createTime: new Date(1000),
        updateTime: new Date(2000),
      })),
      list: vi.fn(async () => ({ presets: [], nextPageToken: "" })),
      update: vi.fn(async () => undefined),
      delete: vi.fn(async () => undefined),
    },
    listModels: vi.fn(async (): Promise<ModelCatalogEntry[]> => [
      { id: "glm-5.2", contextWindow: 1_000_000 },
    ]),
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

describe("resource-name parsers", () => {
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

  it("strips the /agent singleton segment and validates the session underneath", () => {
    expect(parseAgentParent(VALID_AGENT)).toEqual({ template: "saolei", session: "s1" });
    expect(parseAgentParent(VALID)).toBeUndefined();
    expect(parseAgentParent("templates/saolei/sessions/s1/agent/x")).toBeUndefined();
    expect(parseAgentParent("templates/unknown/sessions/s1/agent")).toBeUndefined();
    expect(parseAgentParent("")).toBeUndefined();
  });

  it("validates preset resource names and template parents against the known set", () => {
    expect(parsePresetResource(VALID_PRESET)).toEqual({ template: "saolei", session: "p1" });
    expect(parsePresetResource("templates/saolei/presets/a/b")).toBeUndefined();
    expect(parsePresetResource("templates/unknown/presets/p1")).toBeUndefined();
    expect(parsePresetResource(VALID)).toBeUndefined();
    expect(parseTemplateParent("templates/saolei")).toEqual({ template: "saolei" });
    expect(parseTemplateParent("templates/saolei/presets")).toBeUndefined();
    expect(parseTemplateParent("")).toBeUndefined();
  });
});

describe("AgentService.Send handler", () => {
  it("adapts the grpc call into the TurnStream and dispatches to the sink", () => {
    const deps = fakeDeps();
    const handlers = buildAgentHandlers(deps);
    const fake = invokeSend(handlers, { session: VALID, text: "hello" });

    expect(deps.sessions.send).toHaveBeenCalledTimes(1);
    const [session, text, stream] = (deps.sessions.send as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(session).toBe(VALID);
    expect(text).toBe("hello");

    const event = { session: VALID, turnId: "t1", turnStart: {} } as unknown as ChatEvent;
    stream.write(event);
    expect(fake.written).toEqual([event]);
    stream.end();
    expect(fake.end).toHaveBeenCalled();
  });

  it("rejects a malformed session resource before the stream opens", () => {
    const deps = fakeDeps();
    const handlers = buildAgentHandlers(deps);
    const fake = invokeSend(handlers, { session: "projects/p1", text: "hello" });

    expect(fake.errors).toHaveLength(1);
    expect(fake.errors[0]?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(fake.errors[0]?.details).toContain("templates/{template}/sessions/{session}");
    expect(deps.sessions.send).not.toHaveBeenCalled();
  });

  it("rejects an empty text with INVALID_ARGUMENT", () => {
    const deps = fakeDeps();
    const handlers = buildAgentHandlers(deps);
    const fake = invokeSend(handlers, { session: VALID, text: "" });

    expect(fake.errors).toHaveLength(1);
    expect(fake.errors[0]?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(fake.errors[0]?.details).toContain("non-empty");
    expect(deps.sessions.send).not.toHaveBeenCalled();
  });

  it("maps an unmaterialized session to FAILED_PRECONDITION with the stream never opening", () => {
    // specs/051-agent-v2-dsh-migration/spec.md FR-007: no lazy
    // materialization — the owner exists but no agent does
    // (e.g. after a restart): FAILED_PRECONDITION, HTTP 400 via grpc-gateway
    // (agent-api.md §2.4, data-model.md §3).
    const deps = fakeDeps();
    deps.sessions.send.mockImplementation(() => {
      throw new AgentSessionError(
        "FAILED_PRECONDITION",
        `agent not materialized for session ${VALID}; send UpdateAgent first`,
      );
    });
    const handlers = buildAgentHandlers(deps);
    const fake = invokeSend(handlers, { session: VALID, text: "hello" });

    expect(deps.sessions.send).toHaveBeenCalledTimes(1);
    expect(fake.errors).toHaveLength(1);
    expect(fake.errors[0]?.code).toBe(grpc.status.FAILED_PRECONDITION);
    expect(fake.errors[0]?.details).toContain("UpdateAgent");
    expect(fake.written).toEqual([]);
    expect(fake.end).not.toHaveBeenCalled();
  });
});

describe("AgentService.UpdateAgent handler", () => {
  function updateRequest(overrides: Record<string, unknown> = {}) {
    return {
      agent: { name: VALID_AGENT, preset: VALID_PRESET, model: "" },
      ...overrides,
    };
  }

  it("validates fail-fast then materializes with the preset persona snapshot", async () => {
    const deps = fakeDeps();
    const handlers = buildAgentHandlers(deps);
    const callback = invokeUnary(handlers.UpdateAgent as never, updateRequest({
      agent: { name: VALID_AGENT, preset: VALID_PRESET, model: "glm-5.2" },
    }));

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    // Order: preset lookup → model catalog → materialize (no teardown before
    // validation, data-model.md §2.2).
    expect(deps.presets.get).toHaveBeenCalledWith(VALID_PRESET);
    expect(deps.listModels).toHaveBeenCalledWith("glm-responses");
    expect(deps.sessions.materialize).toHaveBeenCalledWith(VALID, {
      preset: VALID_PRESET,
      model: "glm-5.2",
      persona: "play carefully",
    });
    const [err, response] = callback.mock.calls[0];
    expect(err).toBeNull();
    expect(response).toEqual({
      name: VALID_AGENT,
      preset: VALID_PRESET,
      model: "glm-5.2",
      createTime: { seconds: 1, nanos: 0 },
      updateTime: { seconds: 2, nanos: 0 },
    });
  });

  it("rejects a malformed agent name, an empty preset, and cross-template presets", () => {
    const deps = fakeDeps();
    const handlers = buildAgentHandlers(deps);

    for (const [request, fragment] of [
      [updateRequest({ agent: { name: "sessions/s1/agent", preset: VALID_PRESET } }), "agent resource name"],
      [updateRequest({ agent: { name: VALID_AGENT, preset: "" } }), "agent.preset is required"],
      [updateRequest({ agent: { name: VALID_AGENT, preset: "presets/p1" } }), "preset resource name"],
      [updateRequest({ agent: { name: VALID_AGENT, preset: "templates/unknown/presets/p1" } }), "preset resource name"],
      [updateRequest({ updateMask: { paths: ["name"] } }), "update_mask"],
    ] as Array<[Record<string, unknown>, string]>) {
      const callback = invokeUnary(handlers.UpdateAgent as never, request);
      expect(callback).toHaveBeenCalledTimes(1);
      const error = callback.mock.calls[0][0] as grpc.ServiceError;
      expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
      expect(error?.message).toContain(fragment);
    }
    expect(deps.sessions.materialize).not.toHaveBeenCalled();
    expect(deps.presets.get).not.toHaveBeenCalled();
  });

  it("maps an unknown preset to NOT_FOUND and an unknown model to INVALID_ARGUMENT", async () => {
    const deps = fakeDeps();
    deps.presets.get.mockRejectedValueOnce(
      new PresetStoreError("NOT_FOUND", `preset ${VALID_PRESET} not found`),
    );
    const handlers = buildAgentHandlers(deps);
    const missingPreset = invokeUnary(handlers.UpdateAgent as never, updateRequest());
    await vi.waitFor(() => expect(missingPreset).toHaveBeenCalledTimes(1));
    expect((missingPreset.mock.calls[0][0] as grpc.ServiceError).code).toBe(grpc.status.NOT_FOUND);
    expect(deps.sessions.materialize).not.toHaveBeenCalled();

    const unknownModel = invokeUnary(handlers.UpdateAgent as never, updateRequest({
      agent: { name: VALID_AGENT, preset: VALID_PRESET, model: "glm-9.9" },
    }));
    await vi.waitFor(() => expect(unknownModel).toHaveBeenCalledTimes(1));
    const error = unknownModel.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(error?.message).toContain("unknown model");
    expect(deps.sessions.materialize).not.toHaveBeenCalled();
  });
});

describe("AgentService.GetAgent handler", () => {
  it("returns the materialized configuration", () => {
    const deps = fakeDeps();
    const handlers = buildAgentHandlers(deps);
    const callback = invokeUnary(handlers.GetAgent as never, { name: VALID_AGENT });

    expect(callback).toHaveBeenCalledTimes(1);
    expect(deps.sessions.getAgent).toHaveBeenCalledWith(VALID);
    const [err, response] = callback.mock.calls[0];
    expect(err).toBeNull();
    expect(response?.name).toBe(VALID_AGENT);
    expect(response?.preset).toBe(VALID_PRESET);
  });

  it("rejects a malformed name with INVALID_ARGUMENT and an unmaterialized agent with NOT_FOUND", () => {
    const deps = fakeDeps();
    const handlers = buildAgentHandlers(deps);

    const malformed = invokeUnary(handlers.GetAgent as never, { name: VALID });
    const error = malformed.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);

    deps.sessions.getAgent.mockImplementation(() => {
      throw new AgentSessionError("NOT_FOUND", `agent not materialized for session ${VALID}`);
    });
    const absent = invokeUnary(handlers.GetAgent as never, { name: VALID_AGENT });
    expect((absent.mock.calls[0][0] as grpc.ServiceError).code).toBe(grpc.status.NOT_FOUND);
  });
});

describe("AgentService.ListAgentMessages handler", () => {
  it("wraps the history snapshot in the response with an empty page token", async () => {
    const deps = fakeDeps();
    const messages = [{ messageId: "m1", role: "ROLE_USER" }];
    deps.sessions.listMessages.mockResolvedValue(messages);
    const handlers = buildAgentHandlers(deps);
    const callback = invokeUnary(handlers.ListAgentMessages, { parent: VALID_AGENT });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    // The registry keys entries by the session resource name; the handler
    // strips the /agent singleton segment.
    expect(deps.sessions.listMessages).toHaveBeenCalledWith(VALID);
    expect(callback).toHaveBeenCalledWith(null, { messages, nextPageToken: "" });
  });

  it("rejects a malformed parent with INVALID_ARGUMENT", () => {
    const deps = fakeDeps();
    const handlers = buildAgentHandlers(deps);
    const callback = invokeUnary(handlers.ListAgentMessages, { parent: "nope" });

    expect(callback).toHaveBeenCalledTimes(1);
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(deps.sessions.listMessages).not.toHaveBeenCalled();
  });

  it("maps an unmaterialized agent to NOT_FOUND and other failures to INTERNAL", async () => {
    const deps = fakeDeps();
    deps.sessions.listMessages.mockRejectedValue(
      new AgentSessionError("NOT_FOUND", `agent not materialized for session ${VALID}`),
    );
    const handlers = buildAgentHandlers(deps);
    const absent = invokeUnary(handlers.ListAgentMessages, { parent: VALID_AGENT });
    await vi.waitFor(() => expect(absent).toHaveBeenCalledTimes(1));
    expect((absent.mock.calls[0][0] as grpc.ServiceError).code).toBe(grpc.status.NOT_FOUND);

    deps.sessions.listMessages.mockRejectedValue(new Error("boom"));
    const broken = invokeUnary(handlers.ListAgentMessages, { parent: VALID_AGENT });
    await vi.waitFor(() => expect(broken).toHaveBeenCalledTimes(1));
    const error = broken.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.INTERNAL);
    expect(error?.message).toContain("boom");
  });
});

describe("PresetService preset CRUD handlers", () => {
  it("creates under the parent with server-maintained timestamps and maps ALREADY_EXISTS", async () => {
    const deps = fakeDeps();
    const handlers = buildPresetHandlers(deps);

    const created = invokeUnary(handlers.CreatePreset as never, {
      parent: "templates/saolei",
      presetId: "p1",
      preset: { playerPrompt: "body prompt" },
    });
    await vi.waitFor(() => expect(created).toHaveBeenCalledTimes(1));
    expect(deps.presets.create).toHaveBeenCalledWith({
      name: VALID_PRESET,
      playerPrompt: "body prompt",
      createTime: expect.any(Date),
      updateTime: expect.any(Date),
    });
    const [err, response] = created.mock.calls[0];
    expect(err).toBeNull();
    expect(response?.name).toBe(VALID_PRESET);
    expect(response?.playerPrompt).toBe("body prompt");
    // create_time/update_time are server-maintained at handling time and
    // equal (AIP-133; the update time refreshes on UpdatePreset).
    expect(response?.createTime?.seconds).toBeTypeOf("number");
    expect(response?.updateTime).toEqual(response?.createTime);

    deps.presets.create.mockRejectedValueOnce(
      new PresetStoreError("ALREADY_EXISTS", `preset ${VALID_PRESET} already exists`),
    );
    const duplicate = invokeUnary(handlers.CreatePreset as never, {
      parent: "templates/saolei",
      presetId: "p1",
      preset: {},
    });
    await vi.waitFor(() => expect(duplicate).toHaveBeenCalledTimes(1));
    expect((duplicate.mock.calls[0][0] as grpc.ServiceError).code).toBe(grpc.status.ALREADY_EXISTS);
  });

  it("rejects a malformed parent or preset_id with INVALID_ARGUMENT", () => {
    const deps = fakeDeps();
    const handlers = buildPresetHandlers(deps);

    for (const request of [
      { parent: "templates", presetId: "p1", preset: {} },
      { parent: "templates/unknown", presetId: "p1", preset: {} },
      { parent: "templates/saolei", presetId: "", preset: {} },
      { parent: "templates/saolei", presetId: "a/b", preset: {} },
    ]) {
      const callback = invokeUnary(handlers.CreatePreset as never, request);
      expect((callback.mock.calls[0][0] as grpc.ServiceError).code).toBe(
        grpc.status.INVALID_ARGUMENT,
      );
    }
    expect(deps.presets.create).not.toHaveBeenCalled();
  });

  it("lists under the parent and returns the store's page token", async () => {
    const deps = fakeDeps();
    deps.presets.list.mockResolvedValue({
      presets: [
        { name: VALID_PRESET, playerPrompt: "a", createTime: new Date(1), updateTime: new Date(2) },
      ],
      nextPageToken: VALID_PRESET,
    });
    const handlers = buildPresetHandlers(deps);
    const callback = invokeUnary(handlers.ListPresets as never, {
      parent: "templates/saolei",
      pageSize: 10,
    });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    expect(deps.presets.list).toHaveBeenCalledWith("templates/saolei", 10, "");
    const [err, response] = callback.mock.calls[0];
    expect(err).toBeNull();
    expect(response?.presets).toHaveLength(1);
    expect(response?.nextPageToken).toBe(VALID_PRESET);
  });

  it("gets and deletes by name, mapping the store's NOT_FOUND", async () => {
    const deps = fakeDeps();
    deps.presets.get.mockRejectedValue(
      new PresetStoreError("NOT_FOUND", `preset ${VALID_PRESET} not found`),
    );
    deps.presets.delete.mockRejectedValue(
      new PresetStoreError("NOT_FOUND", `preset ${VALID_PRESET} not found`),
    );
    const handlers = buildPresetHandlers(deps);

    const got = invokeUnary(handlers.GetPreset as never, { name: VALID_PRESET });
    await vi.waitFor(() => expect(got).toHaveBeenCalledTimes(1));
    expect((got.mock.calls[0][0] as grpc.ServiceError).code).toBe(grpc.status.NOT_FOUND);

    const deleted = invokeUnary(handlers.DeletePreset as never, { name: VALID_PRESET });
    await vi.waitFor(() => expect(deleted).toHaveBeenCalledTimes(1));
    expect((deleted.mock.calls[0][0] as grpc.ServiceError).code).toBe(grpc.status.NOT_FOUND);
  });

  it("updates player_prompt, preserves create_time, refreshes update_time, and validates the mask", async () => {
    const deps = fakeDeps();
    const handlers = buildPresetHandlers(deps);

    const updated = invokeUnary(handlers.UpdatePreset as never, {
      preset: { name: VALID_PRESET, playerPrompt: "new prompt" },
      updateMask: { paths: ["player_prompt"] },
    });
    await vi.waitFor(() => expect(updated).toHaveBeenCalledTimes(1));
    expect(deps.presets.update).toHaveBeenCalledWith({
      name: VALID_PRESET,
      playerPrompt: "new prompt",
      createTime: new Date(1000),
      updateTime: expect.any(Date),
    });
    const [err, response] = updated.mock.calls[0];
    expect(err).toBeNull();
    expect(response?.playerPrompt).toBe("new prompt");

    for (const mask of [{ paths: [] }, { paths: ["name"] }]) {
      const rejected = invokeUnary(handlers.UpdatePreset as never, {
        preset: { name: VALID_PRESET, playerPrompt: "x" },
        updateMask: mask,
      });
      expect((rejected.mock.calls[0][0] as grpc.ServiceError).code).toBe(
        grpc.status.INVALID_ARGUMENT,
      );
    }
  });
});

describe("PresetService.ListModels handler", () => {
  it("serves the shared catalog and maps failures to INTERNAL", async () => {
    const deps = fakeDeps();
    const handlers = buildPresetHandlers(deps);

    const callback = invokeUnary(handlers.ListModels as never, {});
    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    expect(deps.listModels).toHaveBeenCalledWith("glm-responses");
    const [err, response] = callback.mock.calls[0];
    expect(err).toBeNull();
    expect(response).toEqual({ models: [{ id: "glm-5.2", contextWindow: 1_000_000 }] });

    deps.listModels.mockRejectedValue(new Error("catalog down"));
    const failed = invokeUnary(handlers.ListModels as never, {});
    await vi.waitFor(() => expect(failed).toHaveBeenCalledTimes(1));
    expect((failed.mock.calls[0][0] as grpc.ServiceError).code).toBe(grpc.status.INTERNAL);
  });
});

describe("DesktopBridgeService.Connect handler", () => {
  it("delegates the bidi stream to the bridge plugin's handlers", () => {
    const connect = vi.fn();
    const handlers = buildDesktopBridgeHandlers({ Connect: connect });
    const call = { on: vi.fn(), write: vi.fn(), end: vi.fn() };
    (handlers.Connect as (c: unknown) => void)(call);
    expect(connect).toHaveBeenCalledTimes(1);
    expect(connect.mock.calls[0][0]).toBe(call);
  });
});
