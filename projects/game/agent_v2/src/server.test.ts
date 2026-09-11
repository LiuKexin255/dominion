import { describe, expect, it, vi } from "vitest";
import * as grpc from "@grpc/grpc-js";
import {
  buildTeamHandlers,
  buildDesktopBridgeHandlers,
  buildPresetHandlers,
  parsePresetResource,
  parseSessionResource,
  parseTeamMemberResource,
  parseTeamResource,
  parseTemplateParent,
  PROTO_PATH,
} from "./server.js";
import type { ModelCatalogEntry } from "./server.js";
import { TeamSessionError } from "./session.js";
import type { TeamView } from "./session.js";
import { PresetAuthoringError } from "@dominion/dsh-preset-authoring";
import type { AgentServiceHandlers } from "../agent_v2_types/projects/game/v2/AgentService.js";
import type { ChatEvent } from "../agent_v2_types/projects/game/v2/ChatEvent.js";
import type { TurnStream } from "./history.js";

/**
 * Handler-level unit tests for the gRPC status mapping (specs/059-agent-v2-
 * team-mode/contracts/team-api.md §1–§6): malformed resource names, missing
 * preset references and empty text are request-level INVALID_ARGUMENT
 * failures (the stream never opens), unmaterialized Sends are
 * FAILED_PRECONDITION, UpdateTeam delegates fail-fast validation and
 * materialization to the team registry, the List faces project the history
 * entries, and the PresetService surface (preset CRUD + ListModels) delegates
 * to the authoring service/catalog with AIP error mapping. The collaborators
 * are `vi.fn()` doubles injected through the buildTeamHandlers/
 * buildPresetHandlers seams — no server binding, no module interception
 * (style/javascript.md Mock convention).
 */

type SendCall = Parameters<AgentServiceHandlers["Send"]>[0];
type UnaryCall = Parameters<AgentServiceHandlers["ListTeamMessages"]>[0];
type UnaryCallback = Parameters<AgentServiceHandlers["ListTeamMessages"]>[1];

const VALID = "templates/saolei/sessions/s1";
const VALID_TEAM = "templates/saolei/sessions/s1/team";
const VALID_MEMBER = "templates/saolei/sessions/s1/team/members/player";
const VALID_PRESET = "templates/saolei/presets/p1";
const P2 = "templates/saolei/presets/p2";

/** The authored-preset projection the authoring service returns. */
function presetView(overrides: Record<string, unknown> = {}) {
  return {
    id: "p1",
    template: "player",
    role: "player",
    persona: "play carefully",
    displayName: undefined,
    createTime: new Date(1000),
    updateTime: new Date(2000),
    ...overrides,
  };
}

/** The team registry projection the session layer returns. */
function teamView(overrides: Partial<TeamView> = {}): TeamView {
  return {
    name: VALID_TEAM,
    members: [
      {
        name: `${VALID_TEAM}/members/player`,
        role: "player",
        preset: VALID_PRESET,
        model: "glm-5.3",
        systemPrompt: "player prompt",
      },
      {
        name: `${VALID_TEAM}/members/planner`,
        role: "planner",
        preset: P2,
        model: "glm-5.3",
        systemPrompt: "planner prompt",
      },
    ],
    createTime: new Date(1000),
    updateTime: new Date(2000),
    ...overrides,
  };
}

function fakeDeps() {
  return {
    sessions: {
      send: vi.fn(() => vi.fn()),
      listTeamMessages: vi.fn(() => [
        {
          member: "user",
          message: { messageId: "m1", role: "ROLE_USER" as const, blocks: [{ text: { content: "hello" } }] },
          seq: 1,
        },
        {
          member: "planner",
          message: { messageId: "m2", role: "ROLE_AGENT" as const, blocks: [{ text: { content: "strategy" } }] },
          seq: 2,
        },
      ]),
      listMemberMessages: vi.fn(() => [
        {
          message: { messageId: "m0", role: "ROLE_USER" as const, blocks: [{ text: { content: "start" } }] },
          sender: "user",
        },
        {
          message: { messageId: "m3", role: "ROLE_USER" as const, blocks: [{ text: { content: "[player] moved" } }] },
          sender: "player",
        },
      ]),
      materialize: vi.fn(async () => teamView()),
      getTeam: vi.fn(() => teamView()),
      getTeamMember: vi.fn(async (_session: string, member: string) => {
        const found = teamView().members.find((entry) => entry.role === member);
        if (found === undefined) {
          throw new TeamSessionError("NOT_FOUND", `team member "${member}" does not exist`);
        }
        return found;
      }),
      cancel: vi.fn(),
    },
    isDesktopConnected: vi.fn(() => true),
    authoring: {
      compose: vi.fn(),
      // Mirror the service: create stamps both timestamps at handling time.
      create: vi.fn(async (input: { persona: string }) =>
        presetView({ persona: input.persona, createTime: new Date(1000), updateTime: new Date(1000) })),
      get: vi.fn(async () => presetView()),
      list: vi.fn(async () => []),
      update: vi.fn(async (_id: string, patch: { persona?: string }) =>
        presetView({ persona: patch.persona ?? "" })),
      remove: vi.fn(async () => undefined),
    },
    listModels: vi.fn(async (): Promise<ModelCatalogEntry[]> => [
      { id: "glm-5.3", contextWindow: 1_000_000 },
      { id: "glm-5.3-flash", contextWindow: 1_000_000 },
    ]),
  };
}

interface FakeCall {
  readonly call: SendCall;
  readonly written: ChatEvent[];
  readonly errors: grpc.ServiceError[];
  readonly end: ReturnType<typeof vi.fn>;
  readonly detach: ReturnType<typeof vi.fn>;
  emitCancelled(): void;
}

function fakeSendCall(
  request: { session?: string; text?: string },
  detach: ReturnType<typeof vi.fn>,
): FakeCall {
  const written: ChatEvent[] = [];
  const errors: grpc.ServiceError[] = [];
  const errorListeners: Array<(err: Error) => void> = [];
  const cancelListeners: Array<() => void> = [];
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
    // The Send handler registers 'error' and 'cancelled' listeners on the
    // real call; the double records them so tests can drive the
    // disconnect path.
    on: vi.fn((name: string, listener: (err?: Error) => void) => {
      if (name === "error") {
        errorListeners.push(listener as (err: Error) => void);
      }
      if (name === "cancelled") {
        cancelListeners.push(listener as () => void);
      }
      return call;
    }),
  };
  return {
    call: call as unknown as SendCall,
    written,
    errors,
    end: call.end,
    detach,
    emitCancelled: () => {
      for (const listener of cancelListeners) {
        listener();
      }
    },
  };
}

function invokeSend(
  handlers: AgentServiceHandlers,
  deps: ReturnType<typeof fakeDeps>,
  request: { session?: string; text?: string },
): FakeCall {
  const detach = vi.fn();
  deps.sessions.send.mockReturnValueOnce(detach);
  const fake = fakeSendCall(request, detach);
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
    // path (tools/release/deploy/README.md §runtime_protos).
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

  it("strips the /team singleton segment and validates the session underneath", () => {
    expect(parseTeamResource(VALID_TEAM)).toEqual({ template: "saolei", session: "s1" });
    expect(parseTeamResource(VALID)).toBeUndefined();
    expect(parseTeamResource("templates/saolei/sessions/s1/team/x")).toBeUndefined();
    expect(parseTeamResource("templates/unknown/sessions/s1/team")).toBeUndefined();
    expect(parseTeamResource("")).toBeUndefined();
  });

  it("parses the member resource shape including the member id", () => {
    expect(parseTeamMemberResource(VALID_MEMBER)).toEqual({
      template: "saolei",
      session: "s1",
      member: "player",
    });
    expect(parseTeamMemberResource(VALID_TEAM)).toBeUndefined();
    expect(parseTeamMemberResource("templates/saolei/sessions/s1/team/members/")).toBeUndefined();
    expect(parseTeamMemberResource("templates/unknown/sessions/s1/team/members/player")).toBeUndefined();
  });

  it("validates preset resource names and template parents against the known set", () => {
    expect(parsePresetResource(VALID_PRESET)).toEqual({ template: "saolei", preset: "p1" });
    expect(parsePresetResource("templates/saolei/presets/a/b")).toBeUndefined();
    expect(parsePresetResource("templates/unknown/presets/p1")).toBeUndefined();
    expect(parsePresetResource(VALID)).toBeUndefined();
    expect(parseTemplateParent("templates/saolei")).toEqual({ template: "saolei" });
    expect(parseTemplateParent("templates/saolei/presets")).toBeUndefined();
    expect(parseTemplateParent("")).toBeUndefined();
  });
});

describe("AgentService.Send handler", () => {
  it("adapts the grpc call into the team stream and dispatches to the sink", () => {
    const deps = fakeDeps();
    const handlers = buildTeamHandlers(deps);
    const fake = invokeSend(handlers, deps, { session: VALID, text: "hello" });

    expect(deps.sessions.send).toHaveBeenCalledTimes(1);
    const [session, text, stream] = (deps.sessions.send as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(session).toBe(VALID);
    expect(text).toBe("hello");

    const event = { session: VALID, turnId: "t1", turnStart: {}, member: "player" } as unknown as ChatEvent;
    stream.write(event);
    expect(fake.written).toEqual([event]);
    stream.end();
    expect(fake.end).toHaveBeenCalled();
  });

  it("rejects a malformed session resource before the stream opens", () => {
    const deps = fakeDeps();
    const handlers = buildTeamHandlers(deps);
    const fake = invokeSend(handlers, deps, { session: "projects/p1", text: "hello" });

    expect(fake.errors).toHaveLength(1);
    expect(fake.errors[0]?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(fake.errors[0]?.details).toContain("templates/{template}/sessions/{session}");
    expect(deps.sessions.send).not.toHaveBeenCalled();
  });

  it("rejects an empty text with INVALID_ARGUMENT", () => {
    const deps = fakeDeps();
    const handlers = buildTeamHandlers(deps);
    const fake = invokeSend(handlers, deps, { session: VALID, text: "" });

    expect(fake.errors).toHaveLength(1);
    expect(fake.errors[0]?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(fake.errors[0]?.details).toContain("non-empty");
    expect(deps.sessions.send).not.toHaveBeenCalled();
  });

  it("maps an unmaterialized session to FAILED_PRECONDITION with the stream never opening", () => {
    const deps = fakeDeps();
    deps.sessions.send.mockImplementationOnce(() => {
      throw new TeamSessionError(
        "FAILED_PRECONDITION",
        `team not materialized for session ${VALID}; send UpdateTeam first`,
      );
    });
    const handlers = buildTeamHandlers(deps);
    const fake = invokeSend(handlers, deps, { session: VALID, text: "hello" });

    expect(fake.errors).toHaveLength(1);
    expect(fake.errors[0]?.code).toBe(grpc.status.FAILED_PRECONDITION);
    expect(fake.errors[0]?.details).toContain("UpdateTeam");
    expect(fake.errors[0]?.cause).toBeInstanceOf(TeamSessionError);
    expect(fake.written).toEqual([]);
    expect(fake.end).not.toHaveBeenCalled();
  });

  it("detaches the subscription on client disconnect without cancelling the team", () => {
    const deps = fakeDeps();
    const handlers = buildTeamHandlers(deps);
    const fake = invokeSend(handlers, deps, { session: VALID, text: "hello" });

    fake.emitCancelled();

    expect(fake.detach).toHaveBeenCalledTimes(1);
    // A disconnect is a subscription concern only: the team keeps running
    // (cancelling is the Cancel RPC's job — contracts/team-api.md §3.3).
    expect(deps.sessions.cancel).not.toHaveBeenCalled();
  });

  it("terminates the stream with an INTERNAL status on a session-reported orchestration failure", () => {
    const deps = fakeDeps();
    const handlers = buildTeamHandlers(deps);
    const fake = invokeSend(handlers, deps, { session: VALID, text: "hello" });
    const stream = (deps.sessions.send as ReturnType<typeof vi.fn>).mock.calls[0][2] as TurnStream;

    stream.fail?.({ code: "INTERNAL", message: "orchestration exploded" });

    expect(fake.errors).toHaveLength(1);
    expect(fake.errors[0]?.code).toBe(grpc.status.INTERNAL);
    expect(fake.errors[0]?.details).toBe("orchestration exploded");
    // The server-issued close is not a peer disconnect: no detach.
    expect(fake.detach).not.toHaveBeenCalled();
  });
});

describe("AgentService.UpdateTeam handler", () => {
  /** The scene-agnostic members-list materialization input. */
  function member(role: string, preset: string, model?: string) {
    return model === undefined ? { role, preset } : { role, preset, model };
  }
  function updateRequest(overrides: Record<string, unknown> = {}) {
    return {
      team: {
        name: VALID_TEAM,
        members: [member("player", VALID_PRESET), member("planner", P2)],
      },
      ...overrides,
    };
  }

  it("accepts the members list and delegates scene validation + materialization to the registry", async () => {
    const deps = fakeDeps();
    const handlers = buildTeamHandlers(deps);
    const callback = invokeUnary(handlers.UpdateTeam as never, updateRequest({
      team: {
        name: VALID_TEAM,
        members: [member("player", VALID_PRESET, "glm-5.3"), member("planner", P2)],
      },
      updateMask: { paths: ["members"] },
    }));

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    expect(deps.sessions.materialize).toHaveBeenCalledWith(VALID, {
      members: [
        { role: "player", preset: VALID_PRESET, model: "glm-5.3" },
        { role: "planner", preset: P2 },
      ],
    });
    const [err, response] = callback.mock.calls[0];
    expect(err).toBeNull();
    expect(response?.name).toBe(VALID_TEAM);
    expect(response?.desktopConnected).toBe(true);
    expect(
      response?.members?.map((wire: { name: string; role: string; preset: string }) => [
        wire.name,
        wire.role,
        wire.preset,
      ]),
    ).toEqual([
      [`${VALID_TEAM}/members/player`, "player", VALID_PRESET],
      [`${VALID_TEAM}/members/planner`, "planner", P2],
    ]);
    expect(response?.members?.[1]?.systemPrompt).toBe("planner prompt");
  });

  it("rejects malformed names and layer-1 structure violations with INVALID_ARGUMENT", () => {
    const deps = fakeDeps();
    const handlers = buildTeamHandlers(deps);

    for (const [request, fragment] of [
      [updateRequest({ team: { name: "sessions/s1/team", members: [] } }), "team resource name"],
      [updateRequest({ team: { name: VALID_TEAM, members: [] } }), "must not be empty"],
      [updateRequest({ team: { name: VALID_TEAM, members: [member("", VALID_PRESET)] } }), "non-empty role"],
      [updateRequest({ team: { name: VALID_TEAM, members: [member("player", "")] } }), "preset resource name"],
      [updateRequest({ team: { name: VALID_TEAM, members: [member("player", "presets/p1")] } }), "preset resource name"],
      [
        updateRequest({ team: { name: VALID_TEAM, members: [member("player", "templates/unknown/presets/p1")] } }),
        "preset resource name",
      ],
      [updateRequest({ updateMask: { paths: ["name"] } }), "update_mask"],
      [updateRequest({ updateMask: { paths: ["members", "name"] } }), "update_mask"],
      // An explicit empty mask is rejected (parity with UpdatePreset):
      // omitting the mask is the way to replace all mutable fields.
      [updateRequest({ updateMask: { paths: [] } }), "update_mask"],
    ] as Array<[Record<string, unknown>, string]>) {
      const callback = invokeUnary(handlers.UpdateTeam as never, request);
      expect(callback).toHaveBeenCalledTimes(1);
      const error = callback.mock.calls[0][0] as grpc.ServiceError;
      expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
      expect(error?.message).toContain(fragment);
    }
    expect(deps.sessions.materialize).not.toHaveBeenCalled();
  });

  it("maps a registry scene INVALID_ARGUMENT through the cause chain", async () => {
    const deps = fakeDeps();
    deps.sessions.materialize.mockRejectedValueOnce(
      new TeamSessionError(
        "INVALID_ARGUMENT",
        'scene check failed: preset "p1" carries role "planner" but member "player" requires the matching role',
      ),
    );
    const handlers = buildTeamHandlers(deps);
    const callback = invokeUnary(handlers.UpdateTeam as never, updateRequest());

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(error?.message).toContain("scene check failed");
    expect(error?.cause).toBeInstanceOf(TeamSessionError);
  });

  it("maps a preset-store INTERNAL through the cause chain (never unknown-preset)", async () => {
    const deps = fakeDeps();
    deps.sessions.materialize.mockRejectedValueOnce(
      new PresetAuthoringError("INTERNAL", "preset store operation failed: mongo unreachable"),
    );
    const handlers = buildTeamHandlers(deps);
    const callback = invokeUnary(handlers.UpdateTeam as never, updateRequest());

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.INTERNAL);
    expect(error?.message).toContain("mongo unreachable");
    expect(error?.cause).toBeInstanceOf(PresetAuthoringError);
  });
});

describe("AgentService.GetTeam / GetTeamMember handlers", () => {
  it("returns the team with the registry's desktop_connected", () => {
    const deps = fakeDeps();
    const handlers = buildTeamHandlers(deps);
    const callback = invokeUnary(handlers.GetTeam as never, { name: VALID_TEAM });

    expect(callback).toHaveBeenCalledTimes(1);
    expect(deps.sessions.getTeam).toHaveBeenCalledWith(VALID);
    expect(deps.isDesktopConnected).toHaveBeenCalledWith(VALID);
    const [err, response] = callback.mock.calls[0];
    expect(err).toBeNull();
    expect(response?.name).toBe(VALID_TEAM);
    expect(response?.desktopConnected).toBe(true);
  });

  it("returns one member including its assembly-read system prompt and rejects unknown members", async () => {
    const deps = fakeDeps();
    const handlers = buildTeamHandlers(deps);
    const callback = invokeUnary(handlers.GetTeamMember as never, { name: VALID_MEMBER });

    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    expect(deps.sessions.getTeamMember).toHaveBeenCalledWith(VALID, "player");
    const [err, response] = callback.mock.calls[0];
    expect(err).toBeNull();
    expect(response?.role).toBe("player");
    expect(response?.systemPrompt).toBe("player prompt");

    const unknown = invokeUnary(handlers.GetTeamMember as never, {
      name: "templates/saolei/sessions/s1/team/members/robot",
    });
    await vi.waitFor(() => expect(unknown).toHaveBeenCalledTimes(1));
    expect((unknown.mock.calls[0][0] as grpc.ServiceError).code).toBe(grpc.status.NOT_FOUND);
  });

  it("maps a member assembly failure to INTERNAL with the cause chain", async () => {
    const deps = fakeDeps();
    deps.sessions.getTeamMember.mockRejectedValueOnce(new Error("assembly exploded"));
    const handlers = buildTeamHandlers(deps);

    const callback = invokeUnary(handlers.GetTeamMember as never, { name: VALID_MEMBER });
    await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
    const error = callback.mock.calls[0][0] as grpc.ServiceError;
    expect(error?.code).toBe(grpc.status.INTERNAL);
    expect(error?.message).toBe("assembly exploded");
    expect(error?.cause).toBeInstanceOf(Error);
  });

  it("rejects malformed names and maps an unmaterialized team to NOT_FOUND", () => {
    const deps = fakeDeps();
    const handlers = buildTeamHandlers(deps);

    const malformed = invokeUnary(handlers.GetTeam as never, { name: VALID });
    expect((malformed.mock.calls[0][0] as grpc.ServiceError).code).toBe(grpc.status.INVALID_ARGUMENT);

    deps.sessions.getTeam.mockImplementationOnce(() => {
      throw new TeamSessionError("NOT_FOUND", `team not materialized for session ${VALID}`);
    });
    const absent = invokeUnary(handlers.GetTeam as never, { name: VALID_TEAM });
    expect((absent.mock.calls[0][0] as grpc.ServiceError).code).toBe(grpc.status.NOT_FOUND);
  });
});

describe("AgentService.ListTeamMessages / ListMemberMessages handlers", () => {
  it("projects the merged sequence with the seq strings and the member view with senders", () => {
    const deps = fakeDeps();
    const handlers = buildTeamHandlers(deps);

    const team = invokeUnary(handlers.ListTeamMessages as never, { parent: VALID_TEAM });
    expect(deps.sessions.listTeamMessages).toHaveBeenCalledWith(VALID);
    const [teamErr, teamResponse] = team.mock.calls[0];
    expect(teamErr).toBeNull();
    // Whole-collection read with the always-empty next_page_token
    // compatibility field (contracts/team-api.md §5).
    expect(teamResponse?.nextPageToken).toBe("");
    expect(teamResponse?.messages?.map((message: { member: string; seq: string }) => [message.member, message.seq])).toEqual([
      ["user", "1"],
      ["planner", "2"],
    ]);
    // member is the wire string label itself: the reserved "user" value for
    // user input, the member role for member output (no enum projection).
    expect(teamResponse?.messages?.[0]?.member).toBe("user");
    expect(teamResponse?.messages?.[0]?.message?.role).toBe("ROLE_USER");
    expect(teamResponse?.messages?.[1]?.message?.role).toBe("ROLE_AGENT");

    const member = invokeUnary(handlers.ListMemberMessages as never, { parent: VALID_MEMBER });
    expect(deps.sessions.listMemberMessages).toHaveBeenCalledWith(VALID, "player");
    const [memberErr, memberResponse] = member.mock.calls[0];
    expect(memberErr).toBeNull();
    expect(memberResponse?.nextPageToken).toBe("");
    // sender is the source role string: the reserved "user" value for user
    // input, the relaying member role for a team-broadcast injection;
    // HistoryMessage.role stays the USER/AGENT view role.
    expect(memberResponse?.messages?.map((message: { sender: string; message: { role: string } }) => [message.sender, message.message.role])).toEqual([
      ["user", "ROLE_USER"],
      ["player", "ROLE_USER"],
    ]);
  });

  it("returns the whole collection regardless of page_size/page_token (compat fields only)", () => {
    const deps = fakeDeps();
    const handlers = buildTeamHandlers(deps);

    // Pagination request fields are protocol compliance only: the in-memory
    // projection is returned whole and next_page_token stays empty
    // (contracts/team-api.md §5 分页语义与现状一致).
    const team = invokeUnary(handlers.ListTeamMessages as never, {
      parent: VALID_TEAM,
      pageSize: 1,
      pageToken: "templates/saolei/sessions/s1/team/messages/1",
    });
    expect(team.mock.calls[0][0]).toBeNull();
    expect(team.mock.calls[0][1]?.messages).toHaveLength(2);
    expect(team.mock.calls[0][1]?.nextPageToken).toBe("");

    const member = invokeUnary(handlers.ListMemberMessages as never, {
      parent: VALID_MEMBER,
      pageSize: 1,
      pageToken: "templates/saolei/sessions/s1/team/members/player/messages/1",
    });
    expect(member.mock.calls[0][0]).toBeNull();
    expect(member.mock.calls[0][1]?.messages).toHaveLength(2);
    expect(member.mock.calls[0][1]?.nextPageToken).toBe("");
    // The sender annotation is the reserved "user" value for user input.
    expect(member.mock.calls[0][1]?.messages?.[0]?.sender).toBe("user");
  });

  it("rejects malformed/foreign parents and maps an unmaterialized team to NOT_FOUND", () => {
    const deps = fakeDeps();
    const handlers = buildTeamHandlers(deps);

    for (const [handler, parent] of [
      [handlers.ListTeamMessages, "nope"],
      [handlers.ListTeamMessages, VALID_MEMBER],
      [handlers.ListMemberMessages, "nope"],
      [handlers.ListMemberMessages, VALID_TEAM],
    ] as Array<[unknown, string]>) {
      const malformed = invokeUnary(handler as never, { parent });
      expect((malformed.mock.calls[0][0] as grpc.ServiceError).code).toBe(
        grpc.status.INVALID_ARGUMENT,
      );
    }

    deps.sessions.listTeamMessages.mockImplementationOnce(() => {
      throw new TeamSessionError("NOT_FOUND", "team not materialized");
    });
    const absent = invokeUnary(handlers.ListTeamMessages as never, { parent: VALID_TEAM });
    expect((absent.mock.calls[0][0] as grpc.ServiceError).code).toBe(grpc.status.NOT_FOUND);

    deps.sessions.listMemberMessages.mockImplementationOnce(() => {
      throw new TeamSessionError("NOT_FOUND", 'team member "robot" does not exist');
    });
    const unknown = invokeUnary(handlers.ListMemberMessages as never, {
      parent: "templates/saolei/sessions/s1/team/members/robot",
    });
    expect((unknown.mock.calls[0][0] as grpc.ServiceError).code).toBe(grpc.status.NOT_FOUND);
  });
});

describe("AgentService.Cancel handler", () => {
  it("cancels the team and answers the empty CancelResponse", () => {
    const deps = fakeDeps();
    const handlers = buildTeamHandlers(deps);
    const callback = invokeUnary(handlers.Cancel as never, { name: VALID_TEAM });

    expect(deps.sessions.cancel).toHaveBeenCalledWith(VALID);
    const [err, response] = callback.mock.calls[0];
    expect(err).toBeNull();
    expect(response).toEqual({});
  });

  it("rejects a malformed name and maps an unmaterialized team to FAILED_PRECONDITION", () => {
    const deps = fakeDeps();
    const handlers = buildTeamHandlers(deps);

    const malformed = invokeUnary(handlers.Cancel as never, { name: VALID });
    expect((malformed.mock.calls[0][0] as grpc.ServiceError).code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(deps.sessions.cancel).not.toHaveBeenCalled();

    deps.sessions.cancel.mockImplementationOnce(() => {
      throw new TeamSessionError(
        "FAILED_PRECONDITION",
        `team not materialized for session ${VALID}; send UpdateTeam first`,
      );
    });
    const absent = invokeUnary(handlers.Cancel as never, { name: VALID_TEAM });
    expect((absent.mock.calls[0][0] as grpc.ServiceError).code).toBe(grpc.status.FAILED_PRECONDITION);
    expect((absent.mock.calls[0][0] as grpc.ServiceError).cause).toBeInstanceOf(TeamSessionError);
  });
});

describe("PresetService preset CRUD handlers", () => {
  it("creates from the role's pool template and maps ALREADY_EXISTS", async () => {
    const deps = fakeDeps();
    const handlers = buildPresetHandlers(deps);

    const created = invokeUnary(handlers.CreatePreset as never, {
      parent: "templates/saolei",
      presetId: "p1",
      preset: { persona: "body prompt" },
      role: "player",
    });
    await vi.waitFor(() => expect(created).toHaveBeenCalledTimes(1));
    // Creation records the preset in the store over the PLAYER pool template:
    // the role decides the template and lands in the store record
    // (specs/059-agent-v2-team-mode/contracts/preset-api.md §2;
    // specs/060-agent-v2-team-optimize/contracts/preset-derivation.md §1/§2).
    expect(deps.authoring.create).toHaveBeenCalledWith({
      id: "p1",
      template: "player",
      role: "player",
      persona: "body prompt",
    });
    const [err, response] = created.mock.calls[0];
    expect(err).toBeNull();
    expect(response?.name).toBe(VALID_PRESET);
    expect(response?.persona).toBe("body prompt");
    expect(response?.role).toBe("player");
    // create_time/update_time are server-maintained at handling time and
    // equal (AIP-133; the update time refreshes on UpdatePreset).
    expect(response?.createTime?.seconds).toBeTypeOf("number");
    expect(response?.updateTime).toEqual(response?.createTime);

    // The PLANNER role derives its composition from the planner pool template
    // (specs/060-agent-v2-team-optimize/contracts/preset-derivation.md §2).
    deps.authoring.create.mockResolvedValueOnce(
      presetView({ id: "p2", template: "planner", role: "planner" }),
    );
    const planner = invokeUnary(handlers.CreatePreset as never, {
      parent: "templates/saolei",
      presetId: "p2",
      preset: {},
      role: "planner",
    });
    await vi.waitFor(() => expect(planner).toHaveBeenCalledTimes(1));
    expect(deps.authoring.create).toHaveBeenLastCalledWith({
      id: "p2",
      template: "planner",
      role: "planner",
      persona: "",
    });

    deps.authoring.create.mockRejectedValueOnce(
      new PresetAuthoringError("ALREADY_EXISTS", "preset p1 already exists"),
    );
    const duplicate = invokeUnary(handlers.CreatePreset as never, {
      parent: "templates/saolei",
      presetId: "p1",
      preset: {},
      role: "player",
    });
    await vi.waitFor(() => expect(duplicate).toHaveBeenCalledTimes(1));
    expect((duplicate.mock.calls[0][0] as grpc.ServiceError).code).toBe(grpc.status.ALREADY_EXISTS);
  });

  it("requires a concrete role on create (INVALID_ARGUMENT)", async () => {
    const deps = fakeDeps();
    const handlers = buildPresetHandlers(deps);

    for (const role of [undefined, "", "referee"]) {
      const callback = invokeUnary(handlers.CreatePreset as never, {
        parent: "templates/saolei",
        presetId: "p1",
        preset: {},
        role,
      });
      await vi.waitFor(() => expect(callback).toHaveBeenCalledTimes(1));
      const error = callback.mock.calls[0][0] as grpc.ServiceError;
      expect(error?.code).toBe(grpc.status.INVALID_ARGUMENT);
      expect(error?.message).toContain("role is required");
    }
    expect(deps.authoring.create).not.toHaveBeenCalled();
  });

  it("rejects a malformed parent or preset_id with INVALID_ARGUMENT", () => {
    const deps = fakeDeps();
    const handlers = buildPresetHandlers(deps);

    for (const request of [
      { parent: "templates", presetId: "p1", preset: {}, role: "player" },
      { parent: "templates/unknown", presetId: "p1", preset: {}, role: "player" },
      { parent: "templates/saolei", presetId: "", preset: {}, role: "player" },
      { parent: "templates/saolei", presetId: "a/b", preset: {}, role: "player" },
    ]) {
      const callback = invokeUnary(handlers.CreatePreset as never, request);
      expect((callback.mock.calls[0][0] as grpc.ServiceError).code).toBe(
        grpc.status.INVALID_ARGUMENT,
      );
    }
    expect(deps.authoring.create).not.toHaveBeenCalled();
  });

  it("lists with role filtering and in-memory keyset pagination", async () => {
    const deps = fakeDeps();
    deps.authoring.list.mockResolvedValue([
      presetView({ id: "a", persona: "a" }),
      presetView({ id: "b", role: "planner", template: "planner", persona: "b" }),
      presetView({ id: "c", persona: "c" }),
    ]);
    const handlers = buildPresetHandlers(deps);

    const firstPage = invokeUnary(handlers.ListPresets as never, {
      parent: "templates/saolei",
      pageSize: 2,
    });
    await vi.waitFor(() => expect(firstPage).toHaveBeenCalledTimes(1));
    // No role filter: the whole sorted set paginates by id (AIP-158).
    expect(deps.authoring.list).toHaveBeenCalledWith(undefined);
    const [err, response] = firstPage.mock.calls[0];
    expect(err).toBeNull();
    expect(response?.presets.map((preset: { name: string }) => preset.name)).toEqual([
      "templates/saolei/presets/a",
      "templates/saolei/presets/b",
    ]);
    expect(response?.nextPageToken).toBe("templates/saolei/presets/b");

    // The token resumes after the last returned id.
    const secondPage = invokeUnary(handlers.ListPresets as never, {
      parent: "templates/saolei",
      pageSize: 2,
      pageToken: "templates/saolei/presets/b",
    });
    await vi.waitFor(() => expect(secondPage).toHaveBeenCalledTimes(1));
    const [, tail] = secondPage.mock.calls[0];
    expect(tail?.presets.map((preset: { name: string }) => preset.name)).toEqual([
      "templates/saolei/presets/c",
    ]);
    expect(tail?.nextPageToken).toBe("");

    // A role filter narrows the authoring query.
    const planners = invokeUnary(handlers.ListPresets as never, {
      parent: "templates/saolei",
      role: "planner",
    });
    await vi.waitFor(() => expect(planners).toHaveBeenCalledTimes(1));
    expect(deps.authoring.list).toHaveBeenLastCalledWith("planner");
  });

  it("returns an empty page for a page token beyond every id", async () => {
    const deps = fakeDeps();
    deps.authoring.list.mockResolvedValue([
      presetView({ id: "a", persona: "a" }),
      presetView({ id: "b", persona: "b" }),
    ]);
    const handlers = buildPresetHandlers(deps);

    // A stale cursor (tail deleted) or fabricated token past the last id
    // yields an empty page, not a wrap-around to the first page.
    const outOfRange = invokeUnary(handlers.ListPresets as never, {
      parent: "templates/saolei",
      pageToken: "templates/saolei/presets/zzz",
    });
    await vi.waitFor(() => expect(outOfRange).toHaveBeenCalledTimes(1));
    const [err, response] = outOfRange.mock.calls[0];
    expect(err).toBeNull();
    expect(response?.presets).toEqual([]);
    expect(response?.nextPageToken).toBe("");
  });

  it("gets and deletes by name, mapping the authoring NOT_FOUND", async () => {
    const deps = fakeDeps();
    deps.authoring.get.mockRejectedValue(
      new PresetAuthoringError("NOT_FOUND", "preset p1 not found"),
    );
    deps.authoring.remove.mockRejectedValue(
      new PresetAuthoringError("NOT_FOUND", "preset p1 not found"),
    );
    const handlers = buildPresetHandlers(deps);

    const got = invokeUnary(handlers.GetPreset as never, { name: VALID_PRESET });
    await vi.waitFor(() => expect(got).toHaveBeenCalledTimes(1));
    expect(deps.authoring.get).toHaveBeenCalledWith("p1");
    expect((got.mock.calls[0][0] as grpc.ServiceError).code).toBe(grpc.status.NOT_FOUND);

    const deleted = invokeUnary(handlers.DeletePreset as never, { name: VALID_PRESET });
    await vi.waitFor(() => expect(deleted).toHaveBeenCalledTimes(1));
    expect(deps.authoring.remove).toHaveBeenCalledWith("p1");
    expect((deleted.mock.calls[0][0] as grpc.ServiceError).code).toBe(grpc.status.NOT_FOUND);
  });

  it("updates the persona through the authoring service and validates the mask", async () => {
    const deps = fakeDeps();
    deps.authoring.update.mockResolvedValueOnce(presetView({ persona: "new prompt" }));
    const handlers = buildPresetHandlers(deps);

    const updated = invokeUnary(handlers.UpdatePreset as never, {
      preset: { name: VALID_PRESET, persona: "new prompt" },
      updateMask: { paths: ["persona"] },
    });
    await vi.waitFor(() => expect(updated).toHaveBeenCalledTimes(1));
    expect(deps.authoring.update).toHaveBeenCalledWith("p1", { persona: "new prompt" });
    const [err, response] = updated.mock.calls[0];
    expect(err).toBeNull();
    expect(response?.persona).toBe("new prompt");

    // role is immutable: it is not an allowed mask path (preset-api.md §2).
    for (const mask of [{ paths: [] }, { paths: ["name"] }, { paths: ["role"] }]) {
      const rejected = invokeUnary(handlers.UpdatePreset as never, {
        preset: { name: VALID_PRESET, persona: "x" },
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
    expect(response).toEqual({
      models: [
        { id: "glm-5.3", contextWindow: 1_000_000 },
        { id: "glm-5.3-flash", contextWindow: 1_000_000 },
      ],
    });

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
