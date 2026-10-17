/**
 * Flow tests (specs/076-agent-v2-optimize/contracts/flow-orchestration.md
 * §1/§4, data-model.md §2): the saolei edge table judged edge by edge, the
 * decision entry's outcome split (settled → onSettle + edge, resting → rest,
 * failed/aborted → freeze), the recovery chain (an external wake restarts the
 * current activity, and a restart that finds nothing falls back to edge
 * judgment), the rest activity's round reset, and the extension demo (SC-007):
 * a new activity/edge pair — a service member whose drive calls an external
 * service — orchestrated by the same runtime with no code change.
 */

import type { GameEventRecord } from "@dominion/dsh-saolei";
import { describe, expect, it } from "vitest";

import {
  Flow,
  ROUND_STOP_INTERVAL,
  SAOLEI_ACTIVITIES,
  SaoleiFacts,
  createSaoleiFlow,
} from "./flow.js";
import type { Activity, Facts } from "./flow.js";
import type { Outcome } from "./member.js";

function terminal(status: "won" | "lost"): GameEventRecord {
  return {
    status,
    stats: {
      operationCount: 1,
      operationsByType: { click: 1, flag: 0, chord: 0 },
      correctFlags: 0,
      avgOpsPerMine: "N/A",
    },
    endedAt: 1,
  };
}

const REVIEWING: Outcome = { kind: "settled" };

function factsOf(overrides: Partial<Facts> = {}): Facts {
  return {
    terminal: null,
    reviewed: null,
    rounds: 0,
    statsSentFor: null,
    ...overrides,
  };
}

describe("saolei edge table (contracts/flow-orchestration.md §1)", () => {
  const cases: ReadonlyArray<{
    readonly edge: string;
    readonly activity: keyof typeof SAOLEI_ACTIVITIES;
    readonly facts: Facts;
    readonly want: string;
  }> = [
    {
      edge: "plan → play (planner settle, unconditional)",
      activity: "plan",
      facts: factsOf(),
      want: "play",
    },
    {
      edge: "play → announce (new terminal record)",
      activity: "play",
      facts: factsOf({ terminal: terminal("won") }),
      want: "announce",
    },
    {
      edge: "play → play (no new record)",
      activity: "play",
      facts: (() => {
        const record = terminal("won");
        return factsOf({ terminal: record, reviewed: record });
      })(),
      want: "play",
    },
    {
      edge: "announce → review (announcer settle, unconditional)",
      activity: "announce",
      facts: factsOf(),
      want: "review",
    },
    {
      edge: "review → play (rounds < 5)",
      activity: "review",
      facts: factsOf({ rounds: ROUND_STOP_INTERVAL - 1 }),
      want: "play",
    },
    {
      edge: "review → rest (rounds ≥ 5)",
      activity: "review",
      facts: factsOf({ rounds: ROUND_STOP_INTERVAL }),
      want: "rest",
    },
    {
      edge: "rest → announce (new record after a drive settled)",
      activity: "rest",
      facts: factsOf({ terminal: terminal("lost") }),
      want: "announce",
    },
    {
      edge: "rest → play (no new record)",
      activity: "rest",
      facts: (() => {
        const record = terminal("lost");
        return factsOf({ terminal: record, reviewed: record });
      })(),
      want: "play",
    },
  ];

  for (const testCase of cases) {
    it(testCase.edge, () => {
      expect(SAOLEI_ACTIVITIES[testCase.activity].next(testCase.facts)).toBe(
        testCase.want,
      );
    });
  }

  it("binds each activity to its member and marks rest driverless", () => {
    expect(SAOLEI_ACTIVITIES.plan.member).toBe("planner");
    expect(SAOLEI_ACTIVITIES.play.member).toBe("player");
    expect(SAOLEI_ACTIVITIES.announce.member).toBe("announcer");
    expect(SAOLEI_ACTIVITIES.review.member).toBe("planner");
    expect(SAOLEI_ACTIVITIES.rest.member).toBe("player");
    expect(SAOLEI_ACTIVITIES.rest.driverless).toBe(true);
  });
});

describe("Flow decision entry", () => {
  it("runs onSettle then judges the edge on a settled outcome", () => {
    const facts = factsOf({ terminal: terminal("won") });
    const flow = new Flow(SAOLEI_ACTIVITIES, facts, "play");

    expect(flow.decide(REVIEWING)).toEqual({ kind: "start", activity: "announce" });
    expect(flow.position).toBe("announce");
    expect(flow.activeMemberId).toBe("announcer");
  });

  it("rests on a resting outcome without judging edges", () => {
    const facts = factsOf({ terminal: terminal("won") });
    const flow = new Flow(SAOLEI_ACTIVITIES, facts, "play");

    expect(flow.decide({ kind: "resting" })).toEqual({ kind: "rest" });
    expect(flow.position).toBe("play");
  });

  it("freezes on failure and restarts the current activity on the next external wake", () => {
    const facts = factsOf({ terminal: terminal("won") });
    const flow = new Flow(SAOLEI_ACTIVITIES, facts, "review");

    expect(
      flow.decide({
        kind: "failed",
        code: "TRANSIENT",
        detail: { engineCode: "SERVER", message: "boom" },
      }),
    ).toEqual({ kind: "frozen" });
    expect(flow.paused).toBe(true);
    expect(flow.position).toBe("review");
    expect(flow.frozenError).toEqual({
      code: "TRANSIENT",
      detail: { engineCode: "SERVER", message: "boom" },
    });

    // The recovery drive (delivery) settled: restart the current activity.
    expect(flow.decide(null)).toEqual({ kind: "start", activity: "review" });
    expect(flow.position).toBe("review");

    // The restart settles: onSettle consumes the record and the edge judges.
    expect(flow.decide(REVIEWING)).toEqual({ kind: "start", activity: "play" });
    expect(facts.reviewed).toBe(facts.terminal);
    expect(facts.rounds).toBe(1);
    expect(flow.frozenError).toBeNull();
    expect(flow.paused).toBe(false);
  });

  it("freezes on aborted without writing a failure fact", () => {
    const flow = new Flow(SAOLEI_ACTIVITIES, factsOf(), "play");

    expect(flow.decide({ kind: "aborted" })).toEqual({ kind: "frozen" });
    expect(flow.paused).toBe(true);
    expect(flow.frozenError).toBeNull();
  });

  it("falls back to edge judgment when the restart finds nothing to execute", () => {
    const flow = new Flow(SAOLEI_ACTIVITIES, factsOf(), "plan");

    flow.decide({ kind: "failed", code: "INTERNAL", detail: { message: "x" } });
    expect(flow.decide(null)).toEqual({ kind: "start", activity: "plan" });
    // The restart drove no content (resting): the flow re-judges the edge.
    expect(flow.decide({ kind: "resting" })).toEqual({ kind: "start", activity: "play" });
    expect(flow.position).toBe("play");
  });

  it("re-judges edges on an external wake while not frozen", () => {
    const facts = factsOf({ terminal: terminal("won") });
    const flow = new Flow(SAOLEI_ACTIVITIES, facts, "plan");

    // The first user send drove the plan activity's member (an external turn).
    expect(flow.decide(null)).toEqual({ kind: "start", activity: "play" });
    expect(facts.reviewed).toBeNull();
  });

  it("clears the round counter when entering rest and recounts from zero", () => {
    const facts = factsOf({ terminal: terminal("won"), rounds: ROUND_STOP_INTERVAL });
    const flow = new Flow(SAOLEI_ACTIVITIES, facts, "review");

    expect(flow.decide(REVIEWING)).toEqual({ kind: "start", activity: "rest" });
    expect(flow.position).toBe("rest");
    expect(facts.rounds).toBe(0);
    expect(flow.paused).toBe(false);
  });

  it("reads the terminal record live so a swallowed game never re-fires", () => {
    const first = terminal("won");
    let latest: GameEventRecord | null = first;
    const facts = new SaoleiFacts(() => latest);
    const flow = createSaoleiFlow(() => latest);
    expect(flow.facts).toBeInstanceOf(SaoleiFacts);

    facts.reviewed = first;
    latest = terminal("lost");
    expect(SAOLEI_ACTIVITIES.play.next(facts)).toBe("announce");
  });
});

// The extension slot (survey/saolei-member-switch-flow.md §7.3): a service
// member whose drive calls an external service. It is orchestrated by the
// same Flow runtime with no change to flow.ts or the execution layer (SC-007).
interface ServiceFacts extends Facts {
  calls: number;
}

type ServiceActivityId = "invoke" | "collect";

const SERVICE_FACTS: ServiceFacts = {
  terminal: null,
  reviewed: null,
  rounds: 0,
  statsSentFor: null,
  calls: 0,
};

const SERVICE_ACTIVITIES: Readonly<
  Record<ServiceActivityId, Activity<ServiceActivityId, "service">>
> = {
  invoke: {
    id: "invoke",
    member: "service",
    next: (facts) => ((facts as ServiceFacts).calls < 1 ? "invoke" : "collect"),
  },
  collect: {
    id: "collect",
    member: "service",
    driverless: true,
    next: () => null,
  },
};

describe("extension demo (SC-007)", () => {
  it("orchestrates a new activity/edge pair without runtime changes", async () => {
    const flow = new Flow(SERVICE_ACTIVITIES, SERVICE_FACTS, "invoke");
    const serviceCalls: string[] = [];
    const executor = async (activity: ServiceActivityId): Promise<Outcome> => {
      if (SERVICE_ACTIVITIES[activity].driverless === true) {
        return { kind: "resting" };
      }
      serviceCalls.push(activity);
      SERVICE_FACTS.calls += 1;
      return { kind: "settled" };
    };

    // The initial wake is external (quiet at rest); the flow judges the edge.
    let decision = flow.decide(null);
    expect(decision).toEqual({ kind: "start", activity: "invoke" });
    decision = flow.decide(await executor("invoke"));
    expect(decision).toEqual({ kind: "start", activity: "collect" });
    expect(serviceCalls).toEqual(["invoke"]);

    // collect has no execution: resting rests the pump.
    expect(flow.decide(await executor("collect"))).toEqual({ kind: "rest" });
  });
});
