/**
 * AgentMember tests (specs/076-agent-v2-optimize/contracts/flow-orchestration.md
 * §2.1/§5): the closed-loop drive (input construction, pre-step batch,
 * completion wait, failure read), the delivery split (in-flight steer vs
 * quiescent assembly with the landed flush), the held-input replay after a
 * failed or aborted drive, the abort channel, and the engine-code → loop-code
 * mapping table. The agent seam is a DI double (`style/javascript.md` Mock
 * convention: no module interception), driven by hand so the assertions read
 * as the exact official-loop event order.
 */

import { LlmError } from "@deepseek-ai/dsh-llm";
import { createUserMessage } from "@deepseek-ai/dsh-llm";
import type { UserMessage } from "@deepseek-ai/dsh-llm";
import { describe, expect, it, vi } from "vitest";

import { AgentMember, mapEngineCode } from "./agent-member.js";
import type { LoopErrorCode } from "./member.js";

/** A relay/user message with the given text (source is irrelevant to the adapter). */
function message(text: string, kind: "user" | "team-broadcast" = "team-broadcast"): UserMessage {
  return createUserMessage({
    content: [{ type: "text", text }],
    source:
      kind === "user"
        ? { kind: "user" }
        : {
            kind: "team-broadcast",
            role: "planner",
            senderSessionId: "s1/planner",
            messageId: `m-${text}` as never,
            form: "relay",
          },
  });
}

function textOf(message: UserMessage): string {
  const block = message.content[0];
  return block !== undefined && block.type === "text" ? block.text : "";
}

/** The hand-driven agent double: `settle()` completes the in-flight turn. */
function createFakeAgent() {
  const waiters: Array<() => void> = [];
  const errorListeners: Array<(payload: { agent: unknown; error: unknown }) => void> = [];
  const injections: UserMessage[] = [];
  const followups: UserMessage[] = [];
  const steers: UserMessage[] = [];
  const warn = vi.fn();

  const agent = {
    id: "planner",
    status: "idle",
    ctx: {
      logger: { warn },
      on: (name: string, listener: (payload: { agent: unknown; error: unknown }) => void) => {
        if (name === "agent/error") {
          errorListeners.push(listener);
        }
        return () => {
          const index = errorListeners.indexOf(listener);
          if (index >= 0) {
            errorListeners.splice(index, 1);
          }
        };
      },
    },
    whenIdle: () => new Promise<void>((resolve) => waiters.push(resolve)),
    inject: (m: UserMessage) => {
      injections.push(m);
    },
    followup: (m: UserMessage) => {
      followups.push(m);
    },
    steer: (m: UserMessage) => {
      steers.push(m);
    },
  };
  return {
    agent,
    injections,
    followups,
    steers,
    warn,
    settle(): void {
      for (const resolve of waiters.splice(0)) {
        resolve();
      }
    },
    failTurn(code: string): void {
      const error = new LlmError(`injected ${code} failure`, code);
      for (const listener of [...errorListeners]) {
        listener({ agent, error });
      }
      this.settle();
    },
  };
}

function createMember(options: {
  readonly drain?: UserMessage[];
  readonly landed?: UserMessage[];
}) {
  const fake = createFakeAgent();
  const drainQueue = options.drain ?? [];
  const landQueue = options.landed ?? [];
  const member = new AgentMember({
    id: "planner",
    agent: fake.agent as never,
    drain: () => drainQueue.splice(0),
    flushLanded: () => landQueue.splice(0),
  });
  return { fake, member, drainQueue, landQueue };
}

describe("mapEngineCode", () => {
  const cases: ReadonlyArray<[string | undefined, LoopErrorCode]> = [
    ["RATE_LIMIT", "TRANSIENT"],
    ["SERVER", "TRANSIENT"],
    ["TIMEOUT", "TRANSIENT"],
    ["TRANSPORT", "TRANSIENT"],
    ["EMPTY_RESPONSE", "TRANSIENT"],
    ["QUOTA", "QUOTA"],
    ["ACCOUNT_QUOTA", "QUOTA"],
    ["INVALID_CREDENTIAL", "CREDENTIAL"],
    ["MISSING_CREDENTIAL", "CREDENTIAL"],
    ["AUTH", "CREDENTIAL"],
    ["NO_ADAPTER", "CONFIG"],
    ["INVALID_REQUEST", "CONFIG"],
    ["HTTP_404", "CONFIG"],
    ["CONTEXT_WINDOW_EXCEEDED", "INPUT_TOO_LARGE"],
    ["IMAGE_OFFLOAD_REQUIRED", "INPUT_TOO_LARGE"],
    ["INVARIANT", "INTERNAL"],
    ["SOMETHING_ELSE", "INTERNAL"],
    [undefined, "INTERNAL"],
  ];

  for (const [engineCode, want] of cases) {
    it(`maps ${engineCode ?? "<none>"} to ${want}`, () => {
      expect(mapEngineCode(engineCode)).toBe(want);
    });
  }
});

describe("AgentMember.drive", () => {
  it("is resting on an empty input and never wakes the agent", async () => {
    const { fake, member } = createMember({});

    const outcome = await member.drive();

    expect(outcome).toEqual({ kind: "resting" });
    expect(fake.followups).toEqual([]);
    expect(fake.injections).toEqual([]);
  });

  it("declares the whole drain set as one pre-step claim (inject all but the last, followup the last)", async () => {
    const { fake, member } = createMember({
      drain: [message("策略一"), message("策略二")],
    });

    const pending = member.drive();
    expect(fake.injections.map(textOf)).toEqual(["策略一"]);
    expect(fake.followups.map(textOf)).toEqual(["策略二"]);
    expect(member.busy).toBe(true);

    fake.settle();
    expect(await pending).toEqual({ kind: "settled" });
    expect(member.busy).toBe(false);
  });

  it("fails loud on a second concurrent drive (the no-in-flight invariant)", async () => {
    const { fake, member } = createMember({ drain: [message("一")] });

    const inFlight = member.drive();
    await expect(member.drive([message("二")])).rejects.toThrow(
      /a member turn is already in flight/,
    );

    fake.settle();
    expect(await inFlight).toEqual({ kind: "settled" });
  });

  it("fails loud when the target agent is not quiescent (the idle invariant)", async () => {
    const { fake, member } = createMember({ drain: [message("一")] });

    fake.agent.status = "running";
    await expect(member.drive()).rejects.toThrow(/not quiescent/);
  });

  it("honors an explicit input override (the held-input replay path)", async () => {
    const { fake, member } = createMember({ drain: [message("drained")] });

    const pending = member.drive([message("explicit")]);
    expect(fake.followups.map(textOf)).toEqual(["explicit"]);
    fake.settle();
    await pending;
  });

  it("reports failed with the loop code and detail when the turn errored", async () => {
    const { fake, member } = createMember({ drain: [message("策略")] });

    const pending = member.drive();
    fake.failTurn("SERVER");

    expect(await pending).toEqual({
      kind: "failed",
      code: "TRANSIENT",
      detail: { engineCode: "SERVER", message: "injected SERVER failure" },
    });
  });

  it("reports aborted when the turn was aborted (no failure fact)", async () => {
    const { fake, member } = createMember({ drain: [message("策略")] });

    const pending = member.drive();
    member.abort();
    fake.settle();

    expect(await pending).toEqual({ kind: "aborted" });
  });

  it("never reports a stale failure marker on the next turn", async () => {
    const { fake, member } = createMember({ drain: [message("一"), message("二")] });

    const first = member.drive();
    fake.failTurn("TIMEOUT");
    expect((await first).kind).toBe("failed");

    const second = member.drive();
    fake.settle();
    expect(await second).toEqual({ kind: "settled" });
  });

  it("replays the held input on a bare drive after a failed turn (flow restart)", async () => {
    const { fake, member } = createMember({ drain: [message("对局记录")] });

    const failed = member.drive();
    fake.failTurn("SERVER");
    await failed;

    // The fresh drain is empty (the team marked the anchors consumed), so the
    // restart replays the held set.
    const retry = member.drive();
    expect(fake.followups.map(textOf)).toEqual(["对局记录", "对局记录"]);
    fake.settle();
    expect(await retry).toEqual({ kind: "settled" });
  });

  it("clears the held set once a drive succeeds", async () => {
    const { fake, member } = createMember({ drain: [message("对局记录")] });

    const first = member.drive();
    fake.failTurn("SERVER");
    await first;
    const retry = member.drive();
    fake.settle();
    await retry;

    const after = member.drive();
    expect(await after).toEqual({ kind: "resting" });
  });
});

describe("AgentMember.deliver", () => {
  it("assembles [drain, landed flush, message] and opens a turn while quiescent", () => {
    const { fake, member } = createMember({
      drain: [message("策略")],
      landed: [message("落地", "user")],
    });

    member.deliver(message("新消息", "user"));

    expect(fake.injections.map(textOf)).toEqual(["策略", "落地"]);
    expect(fake.followups.map(textOf)).toEqual(["新消息"]);
    expect(member.busy).toBe(true);
  });

  it("steers into the in-flight turn and flushes landed first", () => {
    const { fake, member } = createMember({ drain: [message("策略")] });

    member.deliver(message("首条", "user"));
    member.deliver(message("在途", "user"));

    expect(fake.followups.map(textOf)).toEqual(["首条"]);
    expect(fake.steers.map(textOf)).toEqual(["在途"]);
    expect(member.pendingInput).toBe(1);
    expect(member.busy).toBe(true);
  });

  it("clears the steered count when the turn settles", async () => {
    const { fake, member } = createMember({ drain: [message("策略")] });

    const pending = member.drive();
    member.deliver(message("在途", "user"));
    expect(member.pendingInput).toBe(1);

    fake.settle();
    await pending;
    expect(member.pendingInput).toBe(0);
  });

  it("exposes the delivery-started turn through whenSettled", async () => {
    const { fake, member } = createMember({});

    expect(await member.whenSettled()).toEqual({ kind: "resting" });

    member.deliver(message("新消息", "user"));
    const settled = member.whenSettled();
    fake.settle();
    expect(await settled).toEqual({ kind: "settled" });
  });
});

describe("AgentMember.deliverInFlight", () => {
  it("injects into the in-flight turn's next step without waking the driver", async () => {
    const { fake, member } = createMember({
      drain: [message("策略一"), message("策略二")],
    });
    const turn = member.drive();
    expect(member.busy).toBe(true);

    member.deliverInFlight(message("hook 播报"));

    // Non-waking injection: the broadcast joins the next-step inbox while the
    // same turn keeps running — no new turn, no steer, and the delivery
    // bookkeeping does not count it
    // (specs/077-announcer-member-unification/contracts/inflight-broadcast-fastpath.md §5).
    expect(fake.injections.map(textOf)).toEqual(["策略一", "hook 播报"]);
    expect(fake.followups.map(textOf)).toEqual(["策略二"]);
    expect(fake.steers).toEqual([]);
    expect(member.pendingInput).toBe(0);
    expect(member.busy).toBe(true);

    const settled = member.whenSettled();
    fake.settle();
    expect(await turn).toEqual({ kind: "settled" });
    expect(await settled).toEqual({ kind: "settled" });
  });

  it("is a no-op while quiescent (the pending baseline owns the delivery)", () => {
    const { fake, member } = createMember({});
    expect(member.busy).toBe(false);

    member.deliverInFlight(message("hook 播报"));

    expect(fake.injections).toEqual([]);
    expect(fake.followups).toEqual([]);
    expect(member.busy).toBe(false);
    expect(member.pendingInput).toBe(0);
  });

  it("swallows an inject failure with a warning (the announce path is never broken)", async () => {
    const { fake, member } = createMember({ drain: [message("策略")] });
    const turn = member.drive();
    fake.agent.inject = vi.fn(() => {
      throw new Error("inbox closed");
    });

    expect(() => member.deliverInFlight(message("hook 播报"))).not.toThrow();
    expect(fake.warn).toHaveBeenCalledOnce();
    expect(String(fake.warn.mock.calls[0]?.[0])).toContain(
      "in-flight broadcast delivery failed",
    );

    fake.settle();
    expect(await turn).toEqual({ kind: "settled" });
  });
});
