/**
 * MainAgentDriver tests (specs/080-host-main-sub-model/contracts/
 * sts2-loop-minimal.md §4): the quiescent batch assembly, the in-flight steer
 * count, the cancel freeze plus delivery recovery, the failure freeze and the
 * six-code engine classification. The agent seam is a hand-driven DI double
 * (`style/javascript.md` Mock convention: constructor injection, `vi.fn()`
 * call recording, no module interception).
 */

import type { Agent, AgentCancelCause } from "@deepseek-ai/dsh-agent";
import { createUserMessage } from "@deepseek-ai/dsh-llm";
import type { UserMessage } from "@deepseek-ai/dsh-llm";
import { describe, expect, it, vi } from "vitest";

import { MainAgentDriver, mapEngineCode } from "./drive.js";

/** One user message with the given text. */
function message(text: string): UserMessage {
  return createUserMessage({
    content: [{ type: "text", text }],
    source: { kind: "user" },
  });
}

function textOf(value: UserMessage): string {
  const block = value.content[0];
  return block !== undefined && block.type === "text" ? block.text : "";
}

function textsOf(values: readonly UserMessage[]): string[] {
  return values.map(textOf);
}

/**
 * The hand-driven agent double: `whenIdle` resolves on {@link settle}, and
 * `followup` flips the status to `running` the way the official loop does when
 * a wake starts a driver.
 */
function createFakeAgent() {
  type ErrorListener = (payload: { agent: unknown; error: unknown }) => void;
  let status: "idle" | "running" = "idle";
  const waiters: Array<() => void> = [];
  const errorListeners = new Set<ErrorListener>();
  const injections: UserMessage[] = [];
  const followups: UserMessage[] = [];
  const steers: UserMessage[] = [];
  const cancelCauses: AgentCancelCause[] = [];
  // The durable pending lists the driver reads at a failed/cancelled settle
  // (the same structural face saolei's orchestrator reads).
  const nextStep: UserMessage[] = [];
  const nextTurn: UserMessage[] = [];

  const inject = vi.fn((value: UserMessage) => {
    injections.push(value);
    nextStep.push(value);
    status = "running";
  });
  const followup = vi.fn((value: UserMessage) => {
    followups.push(value);
    nextTurn.push(value);
    status = "running";
  });
  const steer = vi.fn((value: UserMessage) => {
    steers.push(value);
    nextStep.push(value);
  });
  const cancel = vi.fn((cause: AgentCancelCause) => {
    cancelCauses.push(cause);
    // The real cancel clears the inbox unless keepInbox is set.
    nextStep.length = 0;
    nextTurn.length = 0;
  });

  const agent = {
    id: "s1/main",
    get status() {
      return status;
    },
    ctx: {
      on: (name: string, listener: ErrorListener) => {
        if (name === "agent/error") {
          errorListeners.add(listener);
        }
        return () => {
          errorListeners.delete(listener);
        };
      },
    },
    inbox: {
      get nextStep() {
        return nextStep;
      },
      get nextTurn() {
        return nextTurn;
      },
      remove: (messageId: UserMessage["id"]) => {
        for (const list of [nextStep, nextTurn]) {
          const index = list.findIndex(
            (message) => String(message.id) === String(messageId),
          );
          if (index >= 0) {
            list.splice(index, 1);
            return true;
          }
        }
        return false;
      },
    },
    whenIdle: () => new Promise<void>((resolve) => waiters.push(resolve)),
    inject,
    followup,
    steer,
    cancel,
  } as unknown as Agent;

  return {
    agent,
    injections,
    followups,
    steers,
    cancelCauses,
    nextStep,
    nextTurn,
    inject,
    followup,
    steer,
    cancel,
    /**
     * Publish one turn failure (the real boundary always precedes idle).
     * `subject` lets a test exercise the identity filter.
     */
    emitError(error: unknown, subject: unknown = agent): void {
      for (const listener of [...errorListeners]) {
        listener({ agent: subject, error });
      }
    },
    fail(error: unknown): void {
      this.emitError(error);
    },
    /** Forget the recorded calls/contents so the next window starts clean. */
    resetCalls(): void {
      injections.length = 0;
      followups.length = 0;
      steers.length = 0;
      cancelCauses.length = 0;
      inject.mockClear();
      followup.mockClear();
      steer.mockClear();
      cancel.mockClear();
    },
    /** Complete the in-flight turn. */
    settle(): void {
      status = "idle";
      for (const resolve of waiters.splice(0)) {
        resolve();
      }
    },
  };
}

type FakeAgent = ReturnType<typeof createFakeAgent>;

/** Await the current turn after settling it through the double. */
async function settleTurn(
  driver: MainAgentDriver,
  fake: FakeAgent,
): Promise<Awaited<ReturnType<MainAgentDriver["whenSettled"]>>> {
  const settled = driver.whenSettled();
  fake.settle();
  return settled;
}

/** An engine failure shaped like a harness error (stable `code` + message). */
function engineError(code: string, text = "engine failed"): Error {
  return Object.assign(new Error(text), { code });
}

describe("MainAgentDriver.submit (quiescent)", () => {
  it("assembles the pre-step batch: inject the prefix, followup the last", async () => {
    // Given a frozen driver whose held set is empty (a quiescent cancel).
    const fake = createFakeAgent();
    const driver = new MainAgentDriver({ agent: fake.agent });
    driver.cancel();
    expect(driver.paused).toBe(true);

    // When a delivery resumes the frozen driver (held set empty here).
    driver.submit(message("hello"));

    // Then the message is the single followup and no prefix was injected.
    expect(driver.busy).toBe(true);
    expect(textsOf(fake.injections)).toEqual([]);
    expect(textsOf(fake.followups)).toEqual(["hello"]);
    expect(driver.paused).toBe(false);

    // And the turn settles back to quiescence (turn_end COMPLETED).
    const outcome = await settleTurn(driver, fake);
    expect(outcome).toEqual({ kind: "settled" });
    expect(driver.busy).toBe(false);
    expect(driver.pendingInput).toBe(0);
    expect(driver.failed).toBe(false);
  });

  it("reports resting when no turn has been submitted", async () => {
    const fake = createFakeAgent();
    const driver = new MainAgentDriver({ agent: fake.agent });

    await expect(driver.whenSettled()).resolves.toEqual({ kind: "resting" });
    expect(fake.followup).not.toHaveBeenCalled();
  });
});

describe("MainAgentDriver.submit (in flight)", () => {
  it("steers into the inbox and counts the pending input", async () => {
    const fake = createFakeAgent();
    const driver = new MainAgentDriver({ agent: fake.agent });
    driver.submit(message("first"));

    // When a second message arrives while the turn is in flight.
    driver.submit(message("second"));

    // Then it steers (no second turn) and the snapshot count reads 1.
    expect(fake.steer).toHaveBeenCalledTimes(1);
    expect(textsOf(fake.steers)).toEqual(["second"]);
    expect(driver.pendingInput).toBe(1);
    expect(driver.busy).toBe(true);

    // And a normal turn end claims the steering: nothing is replayed.
    fake.followup.mockClear();
    const outcome = await settleTurn(driver, fake);
    expect(outcome).toEqual({ kind: "settled" });
    expect(driver.pendingInput).toBe(0);
    expect(fake.followup).not.toHaveBeenCalled();
  });
});

describe("MainAgentDriver.cancel", () => {
  it("cancels the in-flight turn, freezes, and replays held input on delivery", async () => {
    const fake = createFakeAgent();
    const driver = new MainAgentDriver({ agent: fake.agent });
    driver.submit(message("first"));
    driver.submit(message("steered"));

    // When the caller cancels mid-turn.
    driver.cancel();

    // Then the agent receives the user cause and the driver freezes.
    expect(fake.cancel).toHaveBeenCalledTimes(1);
    expect(fake.cancelCauses).toEqual([{ kind: "user" }]);
    expect(driver.paused).toBe(true);
    const aborted = await settleTurn(driver, fake);
    expect(aborted).toEqual({ kind: "aborted" });
    expect(driver.failed).toBe(false);

    // When the user delivers again, the held steering is replayed ahead of it.
    fake.resetCalls();
    driver.submit(message("resume"));
    expect(driver.paused).toBe(false);
    expect(textsOf(fake.injections)).toEqual(["steered"]);
    expect(textsOf(fake.followups)).toEqual(["resume"]);

    const recovered = await settleTurn(driver, fake);
    expect(recovered).toEqual({ kind: "settled" });
  });

  it("freezes a quiescent driver without opening a turn", () => {
    const fake = createFakeAgent();
    const driver = new MainAgentDriver({ agent: fake.agent });

    driver.cancel();

    expect(driver.paused).toBe(true);
    expect(fake.cancel).not.toHaveBeenCalled();
    expect(driver.busy).toBe(false);
    expect(fake.followup).not.toHaveBeenCalled();
  });

  it("is idempotent while frozen", async () => {
    const fake = createFakeAgent();
    const driver = new MainAgentDriver({ agent: fake.agent });
    driver.submit(message("first"));
    driver.cancel();
    driver.cancel();

    // The second cancel sees no active turn and does not touch the agent again.
    expect(fake.cancel).toHaveBeenCalledTimes(1);
    await settleTurn(driver, fake);
    expect(driver.paused).toBe(true);
  });

  it("forwards an explicit cancel cause onto the agent", async () => {
    const fake = createFakeAgent();
    const driver = new MainAgentDriver({ agent: fake.agent });
    driver.submit(message("first"));

    // When the host's teardown path cancels instead of the user.
    driver.cancel({ kind: "disposed" });

    // Then the durable turn/end cause is the caller's, not `user`.
    expect(fake.cancelCauses).toEqual([{ kind: "disposed" }]);
    expect(driver.paused).toBe(true);
    expect(await settleTurn(driver, fake)).toEqual({ kind: "aborted" });
  });

  it("holds a steer that lands while the cancelled turn is winding down", async () => {
    const fake = createFakeAgent();
    const driver = new MainAgentDriver({ agent: fake.agent });
    driver.submit(message("first"));
    driver.cancel();

    // When a delivery still steers into the aborting turn.
    driver.submit(message("late"));

    // Then the aborting settle takes custody of it out of the inbox.
    expect(await settleTurn(driver, fake)).toEqual({ kind: "aborted" });
    expect(textsOf(fake.nextStep)).toEqual([]);

    // And the next delivery replays it ahead of the new message.
    fake.resetCalls();
    driver.submit(message("resume"));
    expect(textsOf(fake.injections)).toEqual(["late"]);
    expect(textsOf(fake.followups)).toEqual(["resume"]);

    await settleTurn(driver, fake);
  });
});

describe("MainAgentDriver failure", () => {
  it("classifies the engine failure, freezes, and clears on the next success", async () => {
    const fake = createFakeAgent();
    const driver = new MainAgentDriver({ agent: fake.agent });
    driver.submit(message("first"));

    // When the turn fails with a transient engine error.
    fake.fail(engineError("RATE_LIMIT", "rate limited"));

    // Then the outcome carries the loop code and the detailed facts.
    const outcome = await settleTurn(driver, fake);
    expect(outcome).toEqual({
      kind: "failed",
      code: "TRANSIENT",
      detail: { engineCode: "RATE_LIMIT", message: "rate limited" },
    });
    expect(driver.failed).toBe(true);
    expect(driver.paused).toBe(true);
    expect(driver.lastError).toEqual({
      message: "rate limited",
      code: "TRANSIENT",
      detail: { engineCode: "RATE_LIMIT", message: "rate limited" },
    });

    // When the user delivers after a failure, the driver resumes without the
    // failed turn's input (only in-flight steering is held; no retry).
    fake.resetCalls();
    driver.submit(message("retry"));
    expect(textsOf(fake.followups)).toEqual(["retry"]);

    // Then a successful turn clears the failure fact.
    const recovered = await settleTurn(driver, fake);
    expect(recovered).toEqual({ kind: "settled" });
    expect(driver.failed).toBe(false);
    expect(driver.lastError).toBeNull();
  });

  it("holds the in-flight steering set for the recovery delivery", async () => {
    const fake = createFakeAgent();
    const driver = new MainAgentDriver({ agent: fake.agent });
    driver.submit(message("first"));
    driver.submit(message("steered"));
    fake.fail(engineError("QUOTA", "quota exhausted"));

    const outcome = await settleTurn(driver, fake);
    expect(outcome).toMatchObject({ kind: "failed", code: "QUOTA" });
    // The unclaimed steering left the durable inbox with the driver's custody:
    // the recovery delivery is its only claim, so it cannot be claimed twice.
    expect(textsOf(fake.nextStep)).toEqual([]);

    // When the user delivers again, the unclaimed steering is replayed first.
    fake.resetCalls();
    driver.submit(message("resume"));
    expect(textsOf(fake.injections)).toEqual(["steered"]);
    expect(textsOf(fake.followups)).toEqual(["resume"]);

    await settleTurn(driver, fake);
  });

  it("does not replay steering the failed turn had already consumed", async () => {
    const fake = createFakeAgent();
    const driver = new MainAgentDriver({ agent: fake.agent });
    driver.submit(message("first"));
    driver.submit(message("consumed"));
    // The turn claimed the steering: the inbox no longer carries it.
    fake.nextStep.length = 0;
    fake.fail(engineError("SERVER", "server error"));

    const outcome = await settleTurn(driver, fake);
    expect(outcome).toMatchObject({ kind: "failed", code: "TRANSIENT" });

    // Then the recovery delivery carries only the new message.
    fake.resetCalls();
    driver.submit(message("resume"));
    expect(textsOf(fake.injections)).toEqual([]);
    expect(textsOf(fake.followups)).toEqual(["resume"]);

    await settleTurn(driver, fake);
  });

  it("maps a non-engine error to INTERNAL", async () => {
    const fake = createFakeAgent();
    const driver = new MainAgentDriver({ agent: fake.agent });
    driver.submit(message("first"));
    fake.fail(new Error("boom"));

    const outcome = await settleTurn(driver, fake);
    expect(outcome).toEqual({
      kind: "failed",
      code: "INTERNAL",
      detail: { message: "boom" },
    });
  });

  it("ignores failures reported for another agent", async () => {
    const fake = createFakeAgent();
    const driver = new MainAgentDriver({ agent: fake.agent });
    driver.submit(message("first"));

    // When another agent's failure reaches the shared context.
    fake.emitError(engineError("QUOTA"), { id: "other" });

    // Then this turn settles normally: the boundary filters by agent identity.
    const outcome = await settleTurn(driver, fake);
    expect(outcome).toEqual({ kind: "settled" });
    expect(driver.failed).toBe(false);
  });
});

describe("MainAgentDriver.dispose", () => {
  it("unsubscribes the failure boundary", async () => {
    const fake = createFakeAgent();
    const driver = new MainAgentDriver({ agent: fake.agent });
    driver.dispose();
    driver.submit(message("first"));

    // A failure published after dispose no longer reaches the driver.
    fake.fail(engineError("QUOTA"));
    const outcome = await settleTurn(driver, fake);
    expect(outcome).toEqual({ kind: "settled" });
    expect(driver.failed).toBe(false);
  });
});

describe("mapEngineCode", () => {
  it.each([
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
    ["UNSUPPORTED_CONTENT", "CONFIG"],
    ["UNSUPPORTED", "CONFIG"],
    ["HTTP_500", "CONFIG"],
    ["CONTEXT_WINDOW_EXCEEDED", "INPUT_TOO_LARGE"],
    ["IMAGE_OFFLOAD_REQUIRED", "INPUT_TOO_LARGE"],
    ["SOMETHING_ELSE", "INTERNAL"],
  ] as const)("maps %s to %s", (engineCode, expected) => {
    expect(mapEngineCode(engineCode)).toBe(expected);
  });

  it("maps an absent code to INTERNAL", () => {
    expect(mapEngineCode(undefined)).toBe("INTERNAL");
  });
});
