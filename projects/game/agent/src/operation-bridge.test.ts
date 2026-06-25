/**
 * operation-bridge.test.ts — Tests for OperationBridge.
 *
 * Covers the core scenarios:
 *   1. register sink → dispatch → handleResult → SUCCEEDED
 *   2. no sink registered → dispatch → 5s timeout → FAILED
 *   3. unregister mid-dispatch → timeout → FAILED
 *
 * Plus additional coverage for sink-throw, unknown result, UUID uniqueness,
 * and concurrent dispatch correlation.
 */

import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";

import { OperationBridge } from "./operation-bridge";

import type { AgentOperationFrame } from "../game_types/projects/game/AgentOperationFrame";
import type { AgentOperationResultFrame } from "../game_types/projects/game/AgentOperationResultFrame";

const STATUS_SUCCEEDED = "AGENT_OPERATION_RESULT_STATUS_SUCCEEDED";
const STATUS_FAILED = "AGENT_OPERATION_RESULT_STATUS_FAILED";

function makeOperation(): AgentOperationFrame {
  return {
    mouse: {
      action: "AGENT_MOUSE_ACTION_LEFT_CLICK",
      xPx: 10,
      yPx: 20,
    },
  };
}

function makeResult(
  operationId: string,
  status: string,
  message = "",
): AgentOperationResultFrame {
  return {
    operationId,
    status: status as AgentOperationResultFrame["status"],
    message,
  };
}

describe("OperationBridge", () => {
  let bridge: OperationBridge;

  beforeEach(() => {
    bridge = new OperationBridge();
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  // ------------------------------------------------------------------
  // Required scenario 1: register sink → dispatch → handleResult → SUCCEEDED
  // ------------------------------------------------------------------
  it("register sink → dispatch → handleResult → resolves with SUCCEEDED", async () => {
    const written: unknown[] = [];
    bridge.registerSink((frame) => {
      written.push(frame);
    });

    const op = makeOperation();
    const promise = bridge.dispatch(op);

    expect(op.operationId).toBeDefined();
    expect(op.operationId).toHaveLength(36);
    expect(written).toHaveLength(1);

    const operationId = op.operationId!;
    bridge.handleResult(makeResult(operationId, STATUS_SUCCEEDED, "ok"));

    const result = await promise;
    expect(result.status).toBe(STATUS_SUCCEEDED);
    expect(result.message).toBe("ok");
  });

  // ------------------------------------------------------------------
  // Required scenario 2: no sink → dispatch → 5s timeout → FAILED
  // ------------------------------------------------------------------
  it("no sink registered → dispatch → 5s timeout → FAILED", async () => {
    const op = makeOperation();
    const promise = bridge.dispatch(op);

    await vi.advanceTimersByTimeAsync(5_000);

    const result = await promise;
    expect(result.status).toBe(STATUS_FAILED);
    expect(result.message).toContain("timed out");
  });

  // ------------------------------------------------------------------
  // Required scenario 3: unregister mid-dispatch → timeout → FAILED
  // ------------------------------------------------------------------
  it("unregister mid-dispatch → timeout → FAILED", async () => {
    const written: unknown[] = [];
    bridge.registerSink((frame) => {
      written.push(frame);
    });

    const op = makeOperation();
    const promise = bridge.dispatch(op);

    expect(written).toHaveLength(1);

    bridge.unregisterSink();

    await vi.advanceTimersByTimeAsync(5_000);

    const result = await promise;
    expect(result.status).toBe(STATUS_FAILED);
    expect(result.message).toContain("timed out");
  });

  // ------------------------------------------------------------------
  // Additional coverage
  // ------------------------------------------------------------------

  it("sink throws during write → immediate FAILED", async () => {
    bridge.registerSink(() => {
      throw new Error("stream closed");
    });

    const op = makeOperation();
    const promise = bridge.dispatch(op);

    const result = await promise;
    expect(result.status).toBe(STATUS_FAILED);
    expect(result.message).toContain("stream closed");
  });

  it("handleResult with unknown operation_id is ignored", async () => {
    bridge.registerSink(() => {});

    const op = makeOperation();
    const promise = bridge.dispatch(op);

    bridge.handleResult(makeResult("nonexistent-id", STATUS_SUCCEEDED, "stale"));

    await vi.advanceTimersByTimeAsync(5_000);

    const result = await promise;
    expect(result.status).toBe(STATUS_FAILED);
  });

  it("dispatch assigns a unique UUID operation_id to each frame", async () => {
    const ids: string[] = [];
    bridge.registerSink((frame) => {
      const op = (frame as { operation?: { operationId?: string } }).operation;
      if (op?.operationId) ids.push(op.operationId);
    });

    const promises = [
      bridge.dispatch(makeOperation()),
      bridge.dispatch(makeOperation()),
      bridge.dispatch(makeOperation()),
    ];

    await vi.advanceTimersByTimeAsync(5_000);
    await Promise.all(promises);

    expect(ids).toHaveLength(3);
    expect(new Set(ids).size).toBe(3);
  });

  it("handleResult resolves the correct pending dispatch when multiple in-flight", async () => {
    bridge.registerSink(() => {});

    const opA = makeOperation();
    const opB = makeOperation();
    const pA = bridge.dispatch(opA);
    const pB = bridge.dispatch(opB);

    bridge.handleResult(makeResult(opB.operationId!, STATUS_SUCCEEDED, "b-done"));
    bridge.handleResult(makeResult(opA.operationId!, STATUS_FAILED, "a-fail"));

    const [rA, rB] = await Promise.all([pA, pB]);
    expect(rA.status).toBe(STATUS_FAILED);
    expect(rA.message).toBe("a-fail");
    expect(rB.status).toBe(STATUS_SUCCEEDED);
    expect(rB.message).toBe("b-done");
  });

  it("written envelope has payload='operation' and carries the operation frame", async () => {
    let captured: { payload?: string; operation?: AgentOperationFrame } | undefined;
    bridge.registerSink((frame) => {
      captured = frame as typeof captured;
    });

    const op = makeOperation();
    const promise = bridge.dispatch(op);

    bridge.handleResult(makeResult(op.operationId!, STATUS_SUCCEEDED));
    await promise;

    expect(captured).toBeDefined();
    expect(captured!.payload).toBe("operation");
    expect(captured!.operation).toBe(op);
    expect(captured!.operation!.operationId).toBe(op.operationId);
  });

  // ------------------------------------------------------------------
  // Screenshot pass-through (Wave 3)
  // ------------------------------------------------------------------

  it("handleResult base64-encodes a Uint8Array screenshot and forwards dimensions", async () => {
    bridge.registerSink(() => {});

    const op = makeOperation();
    const promise = bridge.dispatch(op);

    const pngBytes = Uint8Array.of(0x89, 0x50, 0x4e, 0x47);
    bridge.handleResult({
      operationId: op.operationId!,
      status: STATUS_SUCCEEDED as AgentOperationResultFrame["status"],
      message: "done",
      screenshot: {
        data: pngBytes,
        widthPx: 1920,
        heightPx: 1080,
      },
    });

    const result = await promise;
    expect(result.status).toBe(STATUS_SUCCEEDED);
    expect(result.screenshot).toBeDefined();
    expect(result.screenshot!.data).toBe(Buffer.from(pngBytes).toString("base64"));
    expect(result.screenshot!.widthPx).toBe(1920);
    expect(result.screenshot!.heightPx).toBe(1080);
  });

  it("handleResult passes through an already-string (protojson) screenshot data", async () => {
    bridge.registerSink(() => {});

    const op = makeOperation();
    const promise = bridge.dispatch(op);

    bridge.handleResult({
      operationId: op.operationId!,
      status: STATUS_SUCCEEDED as AgentOperationResultFrame["status"],
      message: "done",
      screenshot: {
        data: "cHJlLWVuY29kZWQ=", // already base64
        widthPx: 800,
        heightPx: 600,
      },
    });

    const result = await promise;
    expect(result.screenshot).toBeDefined();
    expect(result.screenshot!.data).toBe("cHJlLWVuY29kZWQ=");
    expect(result.screenshot!.widthPx).toBe(800);
  });

  it("handleResult omits screenshot when the result frame carries none", async () => {
    bridge.registerSink(() => {});

    const op = makeOperation();
    const promise = bridge.dispatch(op);

    bridge.handleResult(makeResult(op.operationId!, STATUS_SUCCEEDED, "ok"));

    const result = await promise;
    expect(result.screenshot).toBeUndefined();
  });
});
