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
 *
 * Part-model contract: dispatch accepts a Part (MouseMovePart/MouseClickPart),
 * stamps a tool_id, and wraps it in a content PartBlock frame. handleResult
 * accepts a ToolResultPart correlated by tool_id.
 */

import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";

import { OperationBridge } from "./operation-bridge";

import type { Part } from "../game_types/projects/game/Part";
import type { ToolResultPart } from "../game_types/projects/game/ToolResultPart";

const STATUS_SUCCEEDED = "TOOL_RESULT_STATUS_SUCCEEDED";
const STATUS_FAILED = "TOOL_RESULT_STATUS_FAILED";

function makeMovePart(): Part {
  return { mouseMove: { xPx: 10, yPx: 20 } };
}

function makeResult(
  toolId: string,
  status: string,
  message = "",
): ToolResultPart {
  return {
    toolId,
    status: status as ToolResultPart["status"],
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

    const part = makeMovePart();
    const promise = bridge.dispatch(part);

    expect(part.mouseMove!.toolId).toBeDefined();
    expect(part.mouseMove!.toolId).toHaveLength(36);
    expect(written).toHaveLength(1);

    const toolId = part.mouseMove!.toolId!;
    bridge.handleResult(makeResult(toolId, STATUS_SUCCEEDED, "ok"));

    const result = await promise;
    expect(result.status).toBe(STATUS_SUCCEEDED);
    expect(result.message).toBe("ok");
  });

  // ------------------------------------------------------------------
  // Required scenario 2: no sink → dispatch → resolves FAILED (no throw)
  // ------------------------------------------------------------------
  it("no sink registered → dispatch → resolves FAILED", async () => {
    const part = makeMovePart();
    const result = await bridge.dispatch(part);
    expect(result.status).toBe(STATUS_FAILED);
    expect(result.message).toContain("desktop disconnected");
  });

  // ------------------------------------------------------------------
  // Required scenario 3: unregister with the current handle mid-dispatch
  // clears the sink; the pending dispatch then times out → FAILED.
  // ------------------------------------------------------------------
  it("unregister with current handle mid-dispatch → timeout → FAILED", async () => {
    const written: unknown[] = [];
    const handle = bridge.registerSink((frame) => {
      written.push(frame);
    });

    const part = makeMovePart();
    const promise = bridge.dispatch(part);

    expect(written).toHaveLength(1);

    bridge.unregisterSink(handle);

    await vi.advanceTimersByTimeAsync(5_000);

    const result = await promise;
    expect(result.status).toBe(STATUS_FAILED);
    expect(result.message).toContain("timed out");
  });

  // ------------------------------------------------------------------
  // Required scenario 2 (quickstart): sink compare-and-delete prevents a
  // stale stream close from clobbering a fresh registration
  // (specs/021-agent-session-resync/quickstart.md Scenario 2;
  //  specs/021-agent-session-resync/research.md D3).
  // ------------------------------------------------------------------
  it("stale unregister(handleA) after B superseded A leaves sink=B; dispatch routes via B (not FAILED)", async () => {
    const sinkA = vi.fn();
    const sinkB = vi.fn();
    const handleA = bridge.registerSink(sinkA);
    bridge.registerSink(sinkB); // stream-B supersedes stream-A

    // stream-A's late close arrives with its stale handle: compare-and-delete
    // must be a no-op so it cannot null the fresh sink-B.
    bridge.unregisterSink(handleA);

    // sink-B is still the live sink: dispatch writes through B and resolves
    // via it, rather than resolving FAILED "desktop disconnected" (which
    // would mean the sink had been clobbered to null).
    const promise = bridge.dispatch(makeMovePart());

    expect(sinkA).not.toHaveBeenCalled();
    expect(sinkB).toHaveBeenCalledOnce();

    const frame = sinkB.mock.calls[0]![0] as {
      content?: { parts?: { mouseMove?: { toolId?: string } }[] };
    };
    const toolId = frame.content?.parts?.[0]?.mouseMove?.toolId ?? "";
    bridge.handleResult(makeResult(toolId, STATUS_SUCCEEDED, "ok"));

    const result = await promise;
    expect(result.status).toBe(STATUS_SUCCEEDED);
    expect(result.message).toBe("ok");
  });

  it("unregister with current handle clears the sink; subsequent dispatch FAILED 'desktop disconnected'", async () => {
    const sinkB = vi.fn();
    const handleB = bridge.registerSink(sinkB);

    bridge.unregisterSink(handleB);

    const result = await bridge.dispatch(makeMovePart());
    expect(result.status).toBe(STATUS_FAILED);
    expect(result.message).toContain("desktop disconnected");
    expect(sinkB).not.toHaveBeenCalled();
  });

  it("unregisterSink() with no handle is a no-op: sink stays live", async () => {
    // unregisterSink(handle?) JSDoc: omitting handle is a no-op because
    // `this.sink` is never undefined (null or a function), so
    // `this.sink === undefined` is always false (operation-bridge.ts
    // unregisterSink). A call site that forgets the handle must NOT clear a
    // live registration.
    const sink = vi.fn();
    bridge.registerSink(sink);

    bridge.unregisterSink();

    // Sink is still registered: dispatch routes through it and resolves via
    // handleResult, rather than FAILED "desktop disconnected".
    const promise = bridge.dispatch(makeMovePart());
    expect(sink).toHaveBeenCalledOnce();

    const frame = sink.mock.calls[0]![0] as {
      content?: { parts?: { mouseMove?: { toolId?: string } }[] };
    };
    const toolId = frame.content?.parts?.[0]?.mouseMove?.toolId ?? "";
    bridge.handleResult(makeResult(toolId, STATUS_SUCCEEDED, "ok"));

    const result = await promise;
    expect(result.status).toBe(STATUS_SUCCEEDED);
    expect(result.message).toBe("ok");
  });

  // ------------------------------------------------------------------
  // Signal-aware dispatch: abort resolves FAILED without throwing
  // ------------------------------------------------------------------
  it("signal already aborted → dispatch resolves FAILED 'aborted'", async () => {
    bridge.registerSink(() => {});
    const controller = new AbortController();
    controller.abort();
    const part = makeMovePart();
    const result = await bridge.dispatch(part, controller.signal);
    expect(result.status).toBe(STATUS_FAILED);
    expect(result.message).toBe("aborted");
  });

  it("signal aborts mid-dispatch → resolves FAILED before 5s timeout", async () => {
    bridge.registerSink(() => {});
    const controller = new AbortController();
    const part = makeMovePart();
    const promise = bridge.dispatch(part, controller.signal);

    await vi.advanceTimersByTimeAsync(1_000);
    controller.abort();

    const result = await promise;
    expect(result.status).toBe(STATUS_FAILED);
    expect(result.message).toBe("aborted");
  });

  it("handleResult wins over late signal abort (no double-resolve)", async () => {
    let capturedToolId = "";
    bridge.registerSink((frame) => {
      const parts = (frame as { content?: { parts?: { mouseMove?: { toolId?: string } }[] } }).content?.parts ?? [];
      capturedToolId = parts[0]?.mouseMove?.toolId ?? "";
    });
    const controller = new AbortController();
    const part = makeMovePart();
    const promise = bridge.dispatch(part, controller.signal);

    bridge.handleResult(makeResult(capturedToolId, STATUS_SUCCEEDED, "ok"));
    const result = await promise;
    expect(result.status).toBe(STATUS_SUCCEEDED);

    controller.abort();
    expect(result.status).toBe(STATUS_SUCCEEDED);
  });

  // ------------------------------------------------------------------
  // Additional coverage
  // ------------------------------------------------------------------

  it("sink throws during write → immediate FAILED", async () => {
    bridge.registerSink(() => {
      throw new Error("stream closed");
    });

    const part = makeMovePart();
    const promise = bridge.dispatch(part);

    const result = await promise;
    expect(result.status).toBe(STATUS_FAILED);
    expect(result.message).toContain("stream closed");
  });

  it("handleResult with unknown tool_id is ignored", async () => {
    bridge.registerSink(() => {});

    const part = makeMovePart();
    const promise = bridge.dispatch(part);

    bridge.handleResult(makeResult("nonexistent-id", STATUS_SUCCEEDED, "stale"));

    await vi.advanceTimersByTimeAsync(5_000);

    const result = await promise;
    expect(result.status).toBe(STATUS_FAILED);
  });

  it("dispatch assigns a unique UUID tool_id to each part", async () => {
    const ids: string[] = [];
    bridge.registerSink((frame) => {
      const parts = (frame as { content?: { parts?: { mouseMove?: { toolId?: string } }[] } }).content?.parts ?? [];
      const id = parts[0]?.mouseMove?.toolId;
      if (id) ids.push(id);
    });

    const promises = [
      bridge.dispatch(makeMovePart()),
      bridge.dispatch(makeMovePart()),
      bridge.dispatch(makeMovePart()),
    ];

    await vi.advanceTimersByTimeAsync(5_000);
    await Promise.all(promises);

    expect(ids).toHaveLength(3);
    expect(new Set(ids).size).toBe(3);
  });

  it("handleResult resolves the correct pending dispatch when multiple in-flight", async () => {
    bridge.registerSink(() => {});

    const partA = makeMovePart();
    const partB = makeMovePart();
    const pA = bridge.dispatch(partA);
    const pB = bridge.dispatch(partB);

    bridge.handleResult(makeResult(partB.mouseMove!.toolId!, STATUS_SUCCEEDED, "b-done"));
    bridge.handleResult(makeResult(partA.mouseMove!.toolId!, STATUS_FAILED, "a-fail"));

    const [rA, rB] = await Promise.all([pA, pB]);
    expect(rA.status).toBe(STATUS_FAILED);
    expect(rA.message).toBe("a-fail");
    expect(rB.status).toBe(STATUS_SUCCEEDED);
    expect(rB.message).toBe("b-done");
  });

  it("written envelope has payload='content' and carries the tool Part", async () => {
    let captured:
      | { payload?: string; content?: { parts?: Part[] } }
      | undefined;
    bridge.registerSink((frame) => {
      captured = frame as typeof captured;
    });

    const part = makeMovePart();
    const promise = bridge.dispatch(part);

    bridge.handleResult(makeResult(part.mouseMove!.toolId!, STATUS_SUCCEEDED));
    await promise;

    expect(captured).toBeDefined();
    expect(captured!.payload).toBe("content");
    expect(captured!.content).toBeDefined();
    expect(captured!.content!.parts).toHaveLength(1);
    expect(captured!.content!.parts![0]).toBe(part);
    expect(captured!.content!.parts![0].mouseMove!.toolId).toBe(
      part.mouseMove!.toolId,
    );
  });

  // ------------------------------------------------------------------
  // Screenshot pass-through
  // ------------------------------------------------------------------

  it("handleResult base64-encodes a Uint8Array screenshot and forwards dimensions", async () => {
    bridge.registerSink(() => {});

    const part = makeMovePart();
    const promise = bridge.dispatch(part);

    const pngBytes = Uint8Array.of(0x89, 0x50, 0x4e, 0x47);
    bridge.handleResult({
      toolId: part.mouseMove!.toolId!,
      status: STATUS_SUCCEEDED as ToolResultPart["status"],
      message: "done",
      screenshot: {
        encoding: "IMAGE_ENCODING_PNG",
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

    const part = makeMovePart();
    const promise = bridge.dispatch(part);

    bridge.handleResult({
      toolId: part.mouseMove!.toolId!,
      status: STATUS_SUCCEEDED as ToolResultPart["status"],
      message: "done",
      screenshot: {
        encoding: "IMAGE_ENCODING_PNG",
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

  it("handleResult omits screenshot when the result part carries none", async () => {
    bridge.registerSink(() => {});

    const part = makeMovePart();
    const promise = bridge.dispatch(part);

    bridge.handleResult(makeResult(part.mouseMove!.toolId!, STATUS_SUCCEEDED, "ok"));

    const result = await promise;
    expect(result.screenshot).toBeUndefined();
  });
});
