/**
 * Tests for the desktop bridge core (attach/接管/断连结算/超时/abort/uuid
 * tool_id/stale 回执) with the v2 session-keyed registry semantics
 * (specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md §7.2;
 * contracts/desktop-bridge.md §2, data-model.md §2.6). Streams are injected
 * doubles — no module interception (style/javascript.md Mock convention).
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { installReporter, type Reporter } from "@dominion/common-js-logs";

import { DesktopBridge, deriveSessionName } from "./bridge.js";
import type {
  BidiStream,
  BridgeTeamFrame,
  BridgeUserFrame,
  SendFrameCall,
  StreamCall,
  WireFlowPart,
  WireFlowResultPart,
} from "./wire.js";

const STATUS_ACTIVE = "STATUS_SIGNAL_STATUS_ACTIVE";
const STATUS_SUCCEEDED = "TOOL_RESULT_STATUS_SUCCEEDED";
const STATUS_FAILED = "TOOL_RESULT_STATUS_FAILED";

/** Frames written by attach's connection confirmation (the status frame at
 * index 0); dispatch frames follow from index 1. */
const CONFIRM_FRAME_COUNT = 1;

interface FakeCall extends BidiStream, StreamCall {
  readonly written: BridgeTeamFrame[];
  emit(event: "data", frame: BridgeUserFrame): void;
  emitError(err?: unknown): void;
  emitEnd(): void;
  emitClose(): void;
  emitCancelled(): void;
  isEnded(): boolean;
}

function fakeStream(session?: string): FakeCall {
  const written: BridgeTeamFrame[] = [];
  const listeners = new Map<string, Array<(...args: unknown[]) => void>>();
  let ended = false;
  const emit = (event: string, payload?: unknown): void => {
    for (const listener of [...(listeners.get(event) ?? [])]) {
      listener(payload);
    }
  };
  const stream: FakeCall = {
    request: session === undefined ? {} : { session },
    written,
    on(event, listener) {
      const list = listeners.get(event) ?? [];
      list.push(listener as (...args: unknown[]) => void);
      listeners.set(event, list);
      return stream;
    },
    write(frame) {
      written.push(frame);
      return true;
    },
    end() {
      ended = true;
      return stream;
    },
    emit(event, frame) {
      emit(event, frame);
    },
    emitError(err?: unknown) {
      emit("error", err);
    },
    emitEnd() {
      emit("end");
    },
    emitClose() {
      emit("close");
    },
    emitCancelled() {
      emit("cancelled");
    },
    isEnded: () => ended,
  };
  return stream;
}

function probeFrame(session = "s1", template = "saolei"): BridgeUserFrame {
  return {
    sessionId: session,
    templateId: template,
    flowParts: {
      parts: [{ status: { status: "STATUS_SIGNAL_STATUS_ACTIVE" } }],
    },
  };
}

function makeMovePart(): WireFlowPart {
  return { mouseMove: { xPx: 10, yPx: 20 } };
}

function makeResult(
  toolId: string,
  status: string,
  message = "",
): WireFlowResultPart {
  return {
    toolId,
    status: status as WireFlowResultPart["status"],
    message,
  };
}

/** The dispatched part's minted tool_id, read from the stream double's
 * `index`-th written frame (default: the most recent). */
function writtenToolId(stream: FakeCall, index?: number): string {
  const frame =
    index === undefined ? stream.written.at(-1) : stream.written[index];
  const part = frame?.flowParts?.parts?.[0];
  return (
    part?.mouseMove?.toolId ??
    part?.mouseClick?.toolId ??
    part?.keyboardPress?.toolId ??
    part?.mouseMoveAndClick?.toolId ??
    ""
  );
}

describe("DesktopBridge", () => {
  let bridge: DesktopBridge;
  let stream: FakeCall;
  const SESSION = "templates/saolei/sessions/s1";

  beforeEach(() => {
    bridge = new DesktopBridge();
    stream = fakeStream();
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  function attach(): FakeCall {
    bridge.attach(SESSION, stream);
    return stream;
  }

  /** Attach through the Stream handler face (path-bound session identity). */
  function streamAttach(session = SESSION): FakeCall {
    const call = fakeStream(session);
    bridge.handlers().Stream(call);
    return call;
  }

  // ------------------------------------------------------------------
  // attach → dispatch → flow_result 回执 → SUCCEEDED（v1 场景 1 基线）
  // ------------------------------------------------------------------
  it("attach → dispatch → flow_result resolves SUCCEEDED with a minted uuid tool_id", async () => {
    attach();

    const part = makeMovePart();
    const promise = bridge.dispatch(SESSION, part);

    const toolId = writtenToolId(stream);
    expect(toolId).toHaveLength(36);
    // attach's connection-confirmation status frame, then the dispatch frame.
    expect(stream.written).toHaveLength(CONFIRM_FRAME_COUNT + 1);
    expect(stream.written[0]?.flowParts?.parts?.[0]?.status?.status).toBe(STATUS_ACTIVE);

    stream.emit("data", {
      sessionId: "s1",
      templateId: "saolei",
      flowParts: { parts: [{ flowResult: makeResult(toolId, STATUS_SUCCEEDED, "ok") }] },
    });

    const result = await promise;
    expect(result.status).toBe(STATUS_SUCCEEDED);
    expect(result.message).toBe("ok");
  });

  // ------------------------------------------------------------------
  // Connect handler 面首帧绑定（v2 Connect 路径；status 探针不再回显——
  // 上行 part 分派只处理 flow_result/wait/warn，
  // specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §2.9）
  // ------------------------------------------------------------------
  it("handlers().Connect binds on the first frame and ignores the status probe", () => {
    const call = fakeStream();
    bridge.handlers().Connect(call);
    call.emit("data", probeFrame());

    // Only attach's connection confirmation was written: the legacy status
    // probe is warn-ignored rather than echoed.
    expect(call.written).toHaveLength(CONFIRM_FRAME_COUNT);
    const confirm = call.written[0];
    expect(confirm?.flowParts?.parts?.[0]?.status?.status).toBe(STATUS_ACTIVE);
    expect(confirm?.sessionId).toBe(SESSION);
    expect(confirm?.frameId).toBeTruthy();

    // The probe frame still binds the connection: a dispatch now routes
    // through it and its receipt resolves.
    const promise = bridge.dispatch(SESSION, makeMovePart());
    const toolId = writtenToolId(call);
    expect(toolId).not.toBe("");
    call.emit("data", {
      sessionId: "s1",
      templateId: "saolei",
      flowParts: { parts: [{ flowResult: makeResult(toolId, STATUS_SUCCEEDED, "ok") }] },
    });
    return expect(promise).resolves.toMatchObject({ status: STATUS_SUCCEEDED });
  });

  it("Connect frame with no session id is ignored (no binding)", () => {
    const call = fakeStream();
    bridge.handlers().Connect(call);
    call.emit("data", { templateId: "saolei", flowParts: { parts: [] } });

    const result = bridge.dispatch(SESSION, makeMovePart());
    expect(call.written).toHaveLength(0);
    return expect(result).resolves.toMatchObject({
      status: STATUS_FAILED,
      message: "desktop disconnected",
    });
  });

  it("deriveSessionName tolerates a full resource-name sessionId (v1 extractSessionId)", () => {
    expect(deriveSessionName({ sessionId: "templates/saolei/sessions/x", templateId: "saolei" })).toBe(
      "templates/saolei/sessions/x",
    );
    expect(deriveSessionName({ sessionId: "x", templateId: "saolei" })).toBe(
      "templates/saolei/sessions/x",
    );
    expect(deriveSessionName({ templateId: "saolei" })).toBeUndefined();
  });

  // ------------------------------------------------------------------
  // 无连接 → 立即 FAILED（不抛）（v1 场景 2 基线 + US1 场景 4）
  // ------------------------------------------------------------------
  it("no connection → dispatch resolves FAILED 'desktop disconnected' without writing", async () => {
    const result = await bridge.dispatch(SESSION, makeMovePart());
    expect(result.status).toBe(STATUS_FAILED);
    expect(result.message).toBe("desktop disconnected");
  });

  // ------------------------------------------------------------------
  // 20 分钟超时 backstop（v1 场景 3 基线：断连后无回执 → 超时）
  // ------------------------------------------------------------------
  it("no receipt → 20-minute timeout backstop resolves FAILED", async () => {
    attach();
    const promise = bridge.dispatch(SESSION, makeMovePart());
    await vi.advanceTimersByTimeAsync(1_200_000);
    const result = await promise;
    expect(result.status).toBe(STATUS_FAILED);
    expect(result.message).toBe("operation timed out");
  });

  // ------------------------------------------------------------------
  // 断连结算：当前连接断开 → 在途 dispatch FAILED "desktop disconnected"
  // （data-model.md §2.6 状态迁移；v1 留给超时/abort，v2 显式结算）
  // ------------------------------------------------------------------
  it("disconnect settles in-flight dispatches FAILED 'desktop disconnected'", async () => {
    attach();
    const promise = bridge.dispatch(SESSION, makeMovePart());
    expect(stream.written).toHaveLength(CONFIRM_FRAME_COUNT + 1);

    stream.emitClose();

    const result = await promise;
    expect(result.status).toBe(STATUS_FAILED);
    expect(result.message).toBe("desktop disconnected");

    // The registration is cleared: the next dispatch fails immediately.
    const next = await bridge.dispatch(SESSION, makeMovePart());
    expect(next.message).toBe("desktop disconnected");
  });

  it("stream error settles in-flight dispatches like a disconnect", async () => {
    attach();
    const promise = bridge.dispatch(SESSION, makeMovePart());
    stream.emitError(new Error("stream reset"));
    const result = await promise;
    expect(result.status).toBe(STATUS_FAILED);
    expect(result.message).toBe("desktop disconnected");
  });

  // ------------------------------------------------------------------
  // 接管：同 session 新 attach 关闭旧连接；stale close 不能清掉新注册
  // （v1 compare-and-delete 基线 + desktop-bridge.md §6 US3-2）
  // ------------------------------------------------------------------
  it("takeover ends the previous connection; its stale close keeps the fresh one", async () => {
    const streamA = attach();

    const streamB = fakeStream();
    bridge.attach(SESSION, streamB);

    // Takeover closed the old stream.
    expect(streamA.isEnded()).toBe(true);

    // Late close from the superseded stream is a no-op: the fresh
    // registration survives and dispatch still routes through B.
    streamA.emitClose();

    const promise = bridge.dispatch(SESSION, makeMovePart());
    expect(streamB.written).toHaveLength(CONFIRM_FRAME_COUNT + 1);
    const toolId = writtenToolId(streamB);
    streamB.emit("data", {
      sessionId: "s1",
      templateId: "saolei",
      flowParts: { parts: [{ flowResult: makeResult(toolId, STATUS_SUCCEEDED, "ok") }] },
    });
    const result = await promise;
    expect(result.status).toBe(STATUS_SUCCEEDED);
  });

  it("in-flight dispatches survive a takeover and resolve via the new connection", async () => {
    const streamA = attach();
    const promise = bridge.dispatch(SESSION, makeMovePart());
    const toolId = writtenToolId(streamA);

    // New connection takes over BEFORE the receipt arrives.
    const streamB = fakeStream();
    bridge.attach(SESSION, streamB);

    // The receipt arrives on the NEW connection (bidi forwarding continues
    // over the fresh stream) and resolves the dispatch issued on the old one.
    streamB.emit("data", {
      sessionId: "s1",
      templateId: "saolei",
      flowParts: { parts: [{ flowResult: makeResult(toolId, STATUS_SUCCEEDED, "carried over") }] },
    });
    const result = await promise;
    expect(result.status).toBe(STATUS_SUCCEEDED);
    expect(result.message).toBe("carried over");
  });

  // ------------------------------------------------------------------
  // abort 语义（v1 基线）
  // ------------------------------------------------------------------
  it("signal already aborted → dispatch resolves FAILED 'aborted'", async () => {
    attach();
    const controller = new AbortController();
    controller.abort();
    const result = await bridge.dispatch(SESSION, makeMovePart(), controller.signal);
    expect(result.status).toBe(STATUS_FAILED);
    expect(result.message).toBe("aborted");
    // Only attach's confirmation frame was written; the aborted dispatch wrote nothing.
    expect(stream.written).toHaveLength(CONFIRM_FRAME_COUNT);
  });

  it("signal aborts mid-dispatch → resolves FAILED 'aborted' before the timeout", async () => {
    attach();
    const controller = new AbortController();
    const promise = bridge.dispatch(SESSION, makeMovePart(), controller.signal);

    await vi.advanceTimersByTimeAsync(1_000);
    controller.abort();

    const result = await promise;
    expect(result.status).toBe(STATUS_FAILED);
    expect(result.message).toBe("aborted");
  });

  it("receipt wins over a late signal abort (no double-resolve)", async () => {
    attach();
    const controller = new AbortController();
    const promise = bridge.dispatch(SESSION, makeMovePart(), controller.signal);
    const toolId = writtenToolId(stream);

    stream.emit("data", {
      sessionId: "s1",
      templateId: "saolei",
      flowParts: { parts: [{ flowResult: makeResult(toolId, STATUS_SUCCEEDED, "ok") }] },
    });
    const result = await promise;
    expect(result.status).toBe(STATUS_SUCCEEDED);

    controller.abort();
    expect(result.status).toBe(STATUS_SUCCEEDED);
  });

  // ------------------------------------------------------------------
  // 写失败（v1 基线：sink throw → 立即 FAILED）
  // ------------------------------------------------------------------
  it("write throw during dispatch → immediate FAILED with the error message", async () => {
    const throwing = fakeStream();
    throwing.write = () => {
      throw new Error("stream closed");
    };
    bridge.attach(SESSION, throwing);

    const result = await bridge.dispatch(SESSION, makeMovePart());
    expect(result.status).toBe(STATUS_FAILED);
    expect(result.message).toBe("stream closed");
  });

  // ------------------------------------------------------------------
  // stale 回执忽略（v1 基线）
  // ------------------------------------------------------------------
  it("receipt for an unknown tool_id is ignored; the dispatch still times out", async () => {
    attach();
    const promise = bridge.dispatch(SESSION, makeMovePart());
    stream.emit("data", {
      sessionId: "s1",
      templateId: "saolei",
      flowParts: { parts: [{ flowResult: makeResult("nonexistent-id", STATUS_SUCCEEDED, "stale") }] },
    });

    await vi.advanceTimersByTimeAsync(1_200_000);
    const result = await promise;
    expect(result.status).toBe(STATUS_FAILED);
    expect(result.message).toBe("operation timed out");
  });

  it("receipt with no tool_id is ignored", async () => {
    attach();
    const promise = bridge.dispatch(SESSION, makeMovePart());
    stream.emit("data", {
      sessionId: "s1",
      templateId: "saolei",
      flowParts: { parts: [{ flowResult: { status: STATUS_SUCCEEDED, message: "" } }] },
    });

    await vi.advanceTimersByTimeAsync(1_200_000);
    const result = await promise;
    expect(result.status).toBe(STATUS_FAILED);
  });

  // ------------------------------------------------------------------
  // UUID 盖入与并发关联（v1 基线）
  // ------------------------------------------------------------------
  it("each dispatch mints a unique uuid tool_id", async () => {
    attach();
    const ids: string[] = [];
    const promises = [0, 1, 2].map(() => {
      const promise = bridge.dispatch(SESSION, makeMovePart());
      ids.push(writtenToolId(stream));
      return promise;
    });

    expect(new Set(ids).size).toBe(3);
    await vi.advanceTimersByTimeAsync(1_200_000);
    await Promise.all(promises);
  });

  it("receipts resolve the correct pending dispatch when multiple are in flight", async () => {
    attach();
    const partA = makeMovePart();
    const partB = makeMovePart();
    const promiseA = bridge.dispatch(SESSION, partA);
    const promiseB = bridge.dispatch(SESSION, partB);
    const idA = writtenToolId(stream, CONFIRM_FRAME_COUNT);
    const idB = writtenToolId(stream, CONFIRM_FRAME_COUNT + 1);
    expect(idA).not.toBe(idB);

    stream.emit("data", {
      sessionId: "s1",
      templateId: "saolei",
      flowParts: { parts: [{ flowResult: makeResult(idB, STATUS_SUCCEEDED, "b-done") }] },
    });
    stream.emit("data", {
      sessionId: "s1",
      templateId: "saolei",
      flowParts: { parts: [{ flowResult: makeResult(idA, STATUS_FAILED, "a-fail") }] },
    });

    const [resultA, resultB] = await Promise.all([promiseA, promiseB]);
    expect(resultA.status).toBe(STATUS_FAILED);
    expect(resultA.message).toBe("a-fail");
    expect(resultB.status).toBe(STATUS_SUCCEEDED);
    expect(resultB.message).toBe("b-done");
  });

  // ------------------------------------------------------------------
  // 出站帧 envelope（v1 FR-013 语义随迁）
  // ------------------------------------------------------------------
  it("dispatched frame carries the flowParts payload and the session envelope", async () => {
    attach();
    const part = makeMovePart();
    const promise = bridge.dispatch(SESSION, part);
    const toolId = writtenToolId(stream);
    stream.emit("data", {
      sessionId: "s1",
      templateId: "saolei",
      flowParts: { parts: [{ flowResult: makeResult(toolId, STATUS_SUCCEEDED) }] },
    });
    await promise;

    const frame = stream.written[CONFIRM_FRAME_COUNT]!;
    expect(frame.flowParts?.parts).toHaveLength(1);
    expect(frame.flowParts?.parts?.[0]).toBe(part);
    expect(frame.sessionId).toBe(SESSION);
    expect(frame.templateId).toBe("saolei");
    expect(frame.frameId).toBeTruthy();
    expect(frame.createTime?.seconds).toBeGreaterThan(0);
  });

  it("non-operation FlowPart (wait signal) → FAILED 'invalid tool part'", async () => {
    attach();
    const result = await bridge.dispatch(SESSION, { wait: { reason: "x" } });
    expect(result.status).toBe(STATUS_FAILED);
    expect(result.message).toBe("invalid tool part");
    // Only attach's confirmation frame was written.
    expect(stream.written).toHaveLength(CONFIRM_FRAME_COUNT);
  });

  // ------------------------------------------------------------------
  // 截图随迁（v1 基线）
  // ------------------------------------------------------------------
  it("receipt base64-encodes a Uint8Array screenshot and forwards dimensions", async () => {
    attach();
    const promise = bridge.dispatch(SESSION, makeMovePart());
    const toolId = writtenToolId(stream);

    const pngBytes = Uint8Array.of(0x89, 0x50, 0x4e, 0x47);
    stream.emit("data", {
      sessionId: "s1",
      templateId: "saolei",
      flowParts: {
        parts: [
          {
            flowResult: {
              ...makeResult(toolId, STATUS_SUCCEEDED, "done"),
              screenshot: {
                encoding: "IMAGE_ENCODING_PNG",
                data: pngBytes,
                widthPx: 1920,
                heightPx: 1080,
              },
            },
          },
        ],
      },
    });

    const result = await promise;
    expect(result.status).toBe(STATUS_SUCCEEDED);
    expect(result.screenshot?.data).toBe(Buffer.from(pngBytes).toString("base64"));
    expect(result.screenshot?.widthPx).toBe(1920);
    expect(result.screenshot?.heightPx).toBe(1080);
  });

  it("receipt passes through an already-string (protojson) screenshot", async () => {
    attach();
    const promise = bridge.dispatch(SESSION, makeMovePart());
    const toolId = writtenToolId(stream);
    stream.emit("data", {
      sessionId: "s1",
      templateId: "saolei",
      flowParts: {
        parts: [
          {
            flowResult: {
              ...makeResult(toolId, STATUS_SUCCEEDED, "done"),
              screenshot: {
                encoding: "IMAGE_ENCODING_PNG",
                data: "cHJlLWVuY29kZWQ=",
                widthPx: 800,
                heightPx: 600,
              },
            },
          },
        ],
      },
    });

    const result = await promise;
    expect(result.screenshot?.data).toBe("cHJlLWVuY29kZWQ=");
    expect(result.screenshot?.widthPx).toBe(800);
  });

  it("receipt without a screenshot leaves the field absent", async () => {
    attach();
    const promise = bridge.dispatch(SESSION, makeMovePart());
    const toolId = writtenToolId(stream);
    stream.emit("data", {
      sessionId: "s1",
      templateId: "saolei",
      flowParts: { parts: [{ flowResult: makeResult(toolId, STATUS_SUCCEEDED, "ok") }] },
    });
    const result = await promise;
    expect(result.screenshot).toBeUndefined();
  });

  // ------------------------------------------------------------------
  // 会话隔离：另一 session 的回执不串扰（data-model.md §4.6）
  // ------------------------------------------------------------------
  it("receipts for another session do not resolve this session's dispatch", async () => {
    attach();
    const promise = bridge.dispatch(SESSION, makeMovePart());
    const toolId = writtenToolId(stream);

    const other = fakeStream();
    bridge.attach("templates/saolei/sessions/other", other);
    other.emit("data", {
      sessionId: "other",
      templateId: "saolei",
      flowParts: { parts: [{ flowResult: makeResult(toolId, STATUS_SUCCEEDED, "wrong session") }] },
    });

    await vi.advanceTimersByTimeAsync(1_200_000);
    const result = await promise;
    expect(result.status).toBe(STATUS_FAILED);
    expect(result.message).toBe("operation timed out");
  });

  // ------------------------------------------------------------------
  // Stream handler 面：path 绑定 + 首帧 status 确认 + 心跳
  // （specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §2.2/§2.3）
  // ------------------------------------------------------------------
  it("Stream binds the path session and immediately writes a status confirmation frame", () => {
    const call = streamAttach();

    expect(bridge.isDesktopConnected(SESSION)).toBe(true);
    expect(call.written).toHaveLength(CONFIRM_FRAME_COUNT);
    const frame = call.written[0]!;
    expect(frame.flowParts?.parts?.[0]?.status?.status).toBe(STATUS_ACTIVE);
    expect(frame.sessionId).toBe(SESSION);
    expect(frame.templateId).toBe("saolei");
    expect(frame.frameId).toBeTruthy();
    expect(frame.createTime?.seconds).toBeGreaterThan(0);
  });

  it("Stream with no session id warns and registers nothing", () => {
    const call = fakeStream();
    bridge.handlers().Stream(call);

    expect(call.written).toHaveLength(0);
    expect(bridge.isDesktopConnected(SESSION)).toBe(false);
  });

  it("Stream heartbeat writes a status frame every 10s and stops on disconnect", async () => {
    const call = streamAttach();
    expect(call.written).toHaveLength(CONFIRM_FRAME_COUNT);

    await vi.advanceTimersByTimeAsync(10_000);
    expect(call.written).toHaveLength(CONFIRM_FRAME_COUNT + 1);
    await vi.advanceTimersByTimeAsync(10_000);
    expect(call.written).toHaveLength(CONFIRM_FRAME_COUNT + 2);
    for (const frame of call.written.slice(CONFIRM_FRAME_COUNT)) {
      expect(frame.flowParts?.parts?.[0]?.status?.status).toBe(STATUS_ACTIVE);
    }

    call.emitClose();
    expect(bridge.isDesktopConnected(SESSION)).toBe(false);
    await vi.advanceTimersByTimeAsync(30_000);
    expect(call.written).toHaveLength(CONFIRM_FRAME_COUNT + 2);
  });

  it("takeover ends the superseded stream and stops its heartbeat", async () => {
    const first = streamAttach();
    const second = streamAttach();

    expect(first.isEnded()).toBe(true);
    expect(first.written).toHaveLength(CONFIRM_FRAME_COUNT);
    await vi.advanceTimersByTimeAsync(10_000);
    // The superseded stream stopped heartbeating; only the fresh one writes.
    expect(first.written).toHaveLength(CONFIRM_FRAME_COUNT);
    expect(second.written).toHaveLength(CONFIRM_FRAME_COUNT + 1);
    expect(bridge.isDesktopConnected(SESSION)).toBe(true);
  });

  it("late disconnect of a superseded stream is a no-op against the fresh registration", async () => {
    const first = streamAttach();
    const second = streamAttach();

    first.emitClose();

    expect(bridge.isDesktopConnected(SESSION)).toBe(true);
    await vi.advanceTimersByTimeAsync(10_000);
    expect(second.written).toHaveLength(CONFIRM_FRAME_COUNT + 1);
  });

  it("Stream cancelled settles in-flight dispatches FAILED 'desktop disconnected'", async () => {
    const call = streamAttach();
    const promise = bridge.dispatch(SESSION, makeMovePart());

    call.emitCancelled();

    const result = await promise;
    expect(result.status).toBe(STATUS_FAILED);
    expect(result.message).toBe("desktop disconnected");
    expect(bridge.isDesktopConnected(SESSION)).toBe(false);
  });

  // ------------------------------------------------------------------
  // SendFrame handler 面：uplink 单发（无流拒绝/参数校验/回执 resolve/
  // 日志信号/乱序回执）
  // （specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §2.6/§2.7/§2.9）
  // ------------------------------------------------------------------
  function sendFrameCall(frame?: BridgeUserFrame, session?: string): SendFrameCall {
    const request: SendFrameCall["request"] = {};
    if (session !== undefined) {
      request.session = session;
    }
    if (frame !== undefined) {
      request.frame = frame;
    }
    return { request };
  }

  function receiptFrame(toolId: string, message = "ok"): BridgeUserFrame {
    return {
      flowParts: { parts: [{ flowResult: makeResult(toolId, STATUS_SUCCEEDED, message) }] },
    };
  }

  it("SendFrame with no active stream → FAILED_PRECONDITION", () => {
    const callback = vi.fn();
    bridge.SendFrame(sendFrameCall(receiptFrame("op-1"), SESSION), callback);

    expect(callback).toHaveBeenCalledTimes(1);
    expect(callback.mock.calls[0][0]).toMatchObject({
      code: 9,
      message: "desktop stream not active",
    });
  });

  it("SendFrame with no session → INVALID_ARGUMENT", () => {
    const callback = vi.fn();
    bridge.SendFrame(sendFrameCall(receiptFrame("op-1")), callback);

    expect(callback).toHaveBeenCalledTimes(1);
    expect(callback.mock.calls[0][0]).toMatchObject({ code: 3 });
  });

  it("SendFrame with a missing frame or flowParts → INVALID_ARGUMENT", () => {
    attach();
    const invalidCalls: SendFrameCall[] = [
      { request: { session: SESSION } },
      { request: { session: SESSION, frame: { sessionId: "s1" } } },
    ];

    for (const call of invalidCalls) {
      const callback = vi.fn();
      bridge.SendFrame(call, callback);
      expect(callback).toHaveBeenCalledTimes(1);
      expect(callback.mock.calls[0][0]).toMatchObject({ code: 3 });
    }
  });

  it("SendFrame flow_result resolves the matching in-flight dispatch", async () => {
    attach();
    const promise = bridge.dispatch(SESSION, makeMovePart());
    const toolId = writtenToolId(stream);

    const callback = vi.fn();
    bridge.SendFrame(sendFrameCall(receiptFrame(toolId, "done"), SESSION), callback);

    expect(callback).toHaveBeenCalledWith(null, {});
    await expect(promise).resolves.toMatchObject({
      status: STATUS_SUCCEEDED,
      message: "done",
    });
  });

  it("SendFrame wait/warn parts keep their existing log behavior", () => {
    attach();
    const records: Array<{ level: string; msg: string }> = [];
    const reporter: Reporter = {
      write: (level, msg) => {
        records.push({ level, msg });
      },
    };
    const uninstall = installReporter(reporter);
    try {
      const callback = vi.fn();
      bridge.SendFrame(
        sendFrameCall({ flowParts: { parts: [{ wait: { reason: "busy" } }] } }, SESSION),
        callback,
      );
      bridge.SendFrame(
        sendFrameCall({ flowParts: { parts: [{ warn: { message: "careful" } }] } }, SESSION),
        callback,
      );

      expect(records).toContainEqual({
        level: "info",
        msg: "wait signal received from desktop",
      });
      expect(records).toContainEqual({
        level: "warn",
        msg: "warn signal received from desktop",
      });
      expect(callback).toHaveBeenCalledWith(null, {});
    } finally {
      uninstall();
    }
  });

  it("a late SendFrame receipt resolves the older of two in-flight dispatches", async () => {
    attach();
    const first = bridge.dispatch(SESSION, makeMovePart());
    const firstId = writtenToolId(stream, CONFIRM_FRAME_COUNT);
    const second = bridge.dispatch(SESSION, makeMovePart());
    const secondId = writtenToolId(stream, CONFIRM_FRAME_COUNT + 1);
    expect(firstId).not.toBe(secondId);

    // The receipt for the FIRST dispatch arrives only after the second was
    // issued — correlation rides on tool_id, never on arrival order.
    const callback = vi.fn();
    bridge.SendFrame(sendFrameCall(receiptFrame(firstId, "late"), SESSION), callback);

    await expect(first).resolves.toMatchObject({
      status: STATUS_SUCCEEDED,
      message: "late",
    });
    expect(callback).toHaveBeenCalledWith(null, {});

    await vi.advanceTimersByTimeAsync(1_200_000);
    await expect(second).resolves.toMatchObject({
      status: STATUS_FAILED,
      message: "operation timed out",
    });
  });

  it("SendFrame status part is ignored without writing a frame", () => {
    const call = streamAttach();
    const callback = vi.fn();
    bridge.SendFrame(
      sendFrameCall(
        {
          flowParts: {
            parts: [{ status: { status: "STATUS_SIGNAL_STATUS_ACTIVE" } }],
          },
        },
        SESSION,
      ),
      callback,
    );

    // The delivered frame is acked, but a status part (no longer sent by the
    // v2 desktop) writes nothing back: only attach's confirmation frame.
    expect(callback).toHaveBeenCalledWith(null, {});
    expect(call.written).toHaveLength(CONFIRM_FRAME_COUNT);
  });

  it("handlers().SendFrame delegates to the bridge uplink handler", () => {
    streamAttach();
    const callback = vi.fn();
    bridge.handlers().SendFrame(sendFrameCall(receiptFrame("op-1"), SESSION), callback);

    expect(callback).toHaveBeenCalledWith(null, {});
  });
});
