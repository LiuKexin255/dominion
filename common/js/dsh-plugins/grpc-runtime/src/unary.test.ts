/**
 * `callUnary`/`probeChannel` semantics tests (specs/074-dsh-grpc-runtime/
 * tasks.md T003).
 *
 * The invoke callback is a `vi.fn()` DI double; logs are captured through the
 * public `installReporter` seam (style/javascript.md §测试: no `vi.mock`).
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import * as grpc from "@grpc/grpc-js";
import { installReporter } from "@dominion/common-js-logs";
import type {
  LogAttributes,
  LogLevel,
  Reporter,
} from "@dominion/common-js-logs";

import { callUnary, probeChannel } from "./unary.js";
import type { UnaryInvoke } from "./unary.js";

interface LogRecord {
  level: LogLevel;
  msg: string;
  attrs: LogAttributes;
}

function fakeClient(): {
  client: grpc.Client;
  getConnectivityState: ReturnType<typeof vi.fn>;
} {
  const getConnectivityState = vi.fn(() => grpc.connectivityState.READY);
  const channel = { getConnectivityState } as unknown as grpc.Channel;
  const client = {
    getChannel: vi.fn(() => channel),
  } as unknown as grpc.Client;
  return { client, getConnectivityState };
}

function recordingReporter(records: LogRecord[]): Reporter {
  return {
    write: (level: LogLevel, msg: string, attrs: LogAttributes): void => {
      records.push({ level, msg, attrs });
    },
  };
}

let uninstall: () => void;

beforeEach(() => {
  // Silence probe logging by default; tests install a recorder on top.
  uninstall = installReporter({ write: () => {} });
});

afterEach(() => {
  uninstall();
});

describe("callUnary", () => {
  it("invokes with waitForReady and the default deadline", async () => {
    const { client } = fakeClient();
    const invoke = vi.fn<UnaryInvoke<string>>(
      (_c, _m, _o, cb) => cb(null, "ok"),
    );

    const before = Date.now();
    const result = await callUnary(client, "test call", invoke);

    expect(result).toBe("ok");
    expect(invoke).toHaveBeenCalledTimes(1);

    const metadata = invoke.mock.calls[0][1];
    expect(metadata.getOptions().waitForReady).toBe(true);

    const { deadline } = invoke.mock.calls[0][2];
    expect(deadline.getTime() - before).toBeGreaterThanOrEqual(9_000);
    expect(deadline.getTime() - before).toBeLessThanOrEqual(11_000);
  });

  it("honours an explicit deadlineMs override", async () => {
    const { client } = fakeClient();
    const invoke = vi.fn<UnaryInvoke<string>>(
      (_c, _m, _o, cb) => cb(null, "ok"),
    );

    const before = Date.now();
    const result = await callUnary(client, "test call", invoke, 250);

    expect(result).toBe("ok");
    const { deadline } = invoke.mock.calls[0][2];
    expect(deadline.getTime() - before).toBeGreaterThanOrEqual(0);
    expect(deadline.getTime() - before).toBeLessThan(1_000);
  });

  it("probes the channel before the call starts", async () => {
    const { client, getConnectivityState } = fakeClient();
    const invoke = vi.fn<UnaryInvoke<string>>(
      (_c, _m, _o, cb) => cb(null, "ok"),
    );

    await callUnary(client, "test call", invoke);

    expect(getConnectivityState).toHaveBeenCalledTimes(1);
    expect(getConnectivityState.mock.invocationCallOrder[0]).toBeLessThan(
      invoke.mock.invocationCallOrder[0],
    );
  });

  it("rejects and logs the post-failure channel state", async () => {
    const { client, getConnectivityState } = fakeClient();
    const failure = Object.assign(new Error("service unavailable"), {
      code: grpc.status.UNAVAILABLE,
    });
    const invoke = vi.fn<UnaryInvoke<string>>((_c, _m, _o, cb) =>
      cb(failure as grpc.ServiceError),
    );
    const records: LogRecord[] = [];
    const stopRecording = installReporter(recordingReporter(records));

    try {
      await expect(
        callUnary(client, "memory client call", invoke),
      ).rejects.toThrow("service unavailable");
    } finally {
      stopRecording();
    }

    // Pre-call probe + post-failure probe.
    expect(getConnectivityState).toHaveBeenCalledTimes(2);

    const logged = records.find(
      (record) =>
        record.level === "error" &&
        record.msg === "memory client call failed",
    );
    expect(logged).toBeDefined();
    expect(logged?.attrs.error).toBe("service unavailable");
    expect(logged?.attrs.code).toBe(String(grpc.status.UNAVAILABLE));
    expect(logged?.attrs.channelState).toBe("READY");

    // probeChannel logs the state on both probes.
    expect(
      records.filter((record) => record.msg.includes("channel state")),
    ).toHaveLength(2);
  });
});

describe("probeChannel", () => {
  it("returns the connectivity state name and logs it", () => {
    const records: LogRecord[] = [];
    const stopRecording = installReporter(recordingReporter(records));
    const { client } = fakeClient();
    try {
      expect(probeChannel(client, "probe")).toBe("READY");
    } finally {
      stopRecording();
    }
    expect(records[0]?.msg).toBe("probe: channel state");
    expect(records[0]?.attrs.state).toBe("READY");
  });

  it("reports no-channel for an injected client without a channel", () => {
    expect(probeChannel({} as grpc.Client, "probe")).toBe("no-channel");
  });

  it("reports a thrown probe as an error string", () => {
    const client = {
      getChannel: () => {
        throw new Error("boom");
      },
    } as unknown as grpc.Client;
    expect(probeChannel(client, "probe")).toBe("error:boom");
  });
});
