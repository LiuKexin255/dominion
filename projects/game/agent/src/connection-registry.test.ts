/**
 * connection-registry.test.ts — Tests for ConnectionRegistry.
 *
 * Covers: basic register/unregister, kick-old-on-new-register, concurrent
 * registration arbitration, isAlive after kick, and unregister with wrong
 * connection reference.
 */

import { describe, expect, it, vi } from "vitest";

import { ConnectionRegistry } from "./connection-registry";
import type { Connection } from "./connection-registry";

// ---------------------------------------------------------------------------
// Mock helpers
// ---------------------------------------------------------------------------

/** Create a mock Connection whose close() is a vitest spy. */
function createMockConnection(): Connection {
  return { close: vi.fn() };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("ConnectionRegistry.register", () => {
  it("registers a connection and makes it alive", async () => {
    const registry = new ConnectionRegistry();
    const conn = createMockConnection();

    await registry.register("sess-1", conn);

    expect(registry.isAlive("sess-1", conn)).toBe(true);
  });

  it("kicks old connection when new connection registers for same session", async () => {
    const registry = new ConnectionRegistry();
    const conn1 = createMockConnection();
    const conn2 = createMockConnection();

    await registry.register("sess-1", conn1);
    await registry.register("sess-1", conn2);

    // Old connection should be closed (fire-and-forget).
    expect(conn1.close).toHaveBeenCalledTimes(1);
    // New connection is alive.
    expect(registry.isAlive("sess-1", conn2)).toBe(true);
    // Old connection is not alive.
    expect(registry.isAlive("sess-1", conn1)).toBe(false);
  });

  it("closes old connection only once even with multiple replacements", async () => {
    const registry = new ConnectionRegistry();
    const conn1 = createMockConnection();
    const conn2 = createMockConnection();
    const conn3 = createMockConnection();

    await registry.register("sess-1", conn1);
    await registry.register("sess-1", conn2);
    await registry.register("sess-1", conn3);

    expect(conn1.close).toHaveBeenCalledTimes(1);
    expect(conn2.close).toHaveBeenCalledTimes(1);
    expect(conn3.close).not.toHaveBeenCalled();

    expect(registry.isAlive("sess-1", conn1)).toBe(false);
    expect(registry.isAlive("sess-1", conn2)).toBe(false);
    expect(registry.isAlive("sess-1", conn3)).toBe(true);
  });

  it("isAlive returns false for unknown session", () => {
    const registry = new ConnectionRegistry();
    const conn = createMockConnection();

    expect(registry.isAlive("nonexistent", conn)).toBe(false);
  });
});

describe("ConnectionRegistry.unregister", () => {
  it("removes the connection and marks it as not alive", async () => {
    const registry = new ConnectionRegistry();
    const conn = createMockConnection();

    await registry.register("sess-1", conn);
    expect(registry.isAlive("sess-1", conn)).toBe(true);

    registry.unregister("sess-1", conn);
    expect(registry.isAlive("sess-1", conn)).toBe(false);
  });

  it("is a no-op for an already-replaced connection", async () => {
    const registry = new ConnectionRegistry();
    const conn1 = createMockConnection();
    const conn2 = createMockConnection();

    await registry.register("sess-1", conn1);
    await registry.register("sess-1", conn2);

    // Unregister the old (already kicked) connection — should be no-op.
    registry.unregister("sess-1", conn1);

    // conn2 should still be alive.
    expect(registry.isAlive("sess-1", conn2)).toBe(true);
  });

  it("is a no-op for wrong connection reference", async () => {
    const registry = new ConnectionRegistry();
    const conn1 = createMockConnection();
    const conn2 = createMockConnection();

    await registry.register("sess-1", conn1);

    // Try to unregister a different connection object.
    registry.unregister("sess-1", conn2);

    // Original should still be alive.
    expect(registry.isAlive("sess-1", conn1)).toBe(true);
  });

  it("is a no-op for unknown session", () => {
    const registry = new ConnectionRegistry();
    const conn = createMockConnection();

    // Should not throw.
    registry.unregister("nonexistent", conn);
  });
});

describe("ConnectionRegistry concurrent arbitration", () => {
  it("exactly one connection survives concurrent register calls (same session)", async () => {
    const registry = new ConnectionRegistry();
    const conns = [createMockConnection(), createMockConnection(), createMockConnection()];

    // Fire concurrent registrations.
    await Promise.all(conns.map((c) => registry.register("sess-1", c)));

    // Count survivors.
    const survivors = conns.filter((c) => registry.isAlive("sess-1", c));
    expect(survivors).toHaveLength(1);

    // All kicked connections had close() called.
    const kicked = conns.filter((c) => !registry.isAlive("sess-1", c));
    for (const c of kicked) {
      expect(c.close).toHaveBeenCalled();
    }

    // The survivor had close() NOT called.
    const survivor = survivors[0];
    expect(survivor.close).not.toHaveBeenCalled();
  });

  it("serializes concurrent register calls for different sessions independently", async () => {
    const registry = new ConnectionRegistry();
    const connA1 = createMockConnection();
    const connA2 = createMockConnection();
    const connB1 = createMockConnection();
    const connB2 = createMockConnection();

    // Even with concurrent registration on two sessions, each session resolves to one survivor.
    await Promise.all([
      Promise.all([
        registry.register("sess-A", connA1),
        registry.register("sess-A", connA2),
      ]),
      Promise.all([
        registry.register("sess-B", connB1),
        registry.register("sess-B", connB2),
      ]),
    ]);

    // Exactly one survivor per session.
    const survivorsA = [connA1, connA2].filter((c) => registry.isAlive("sess-A", c));
    const survivorsB = [connB1, connB2].filter((c) => registry.isAlive("sess-B", c));

    expect(survivorsA).toHaveLength(1);
    expect(survivorsB).toHaveLength(1);

    // Sessions are independent.
    expect(survivorsA[0]).not.toBe(survivorsB[0]);
  });
});

describe("ConnectionRegistry isAlive after kick", () => {
  it("returns false for kicked connection after replacement", async () => {
    const registry = new ConnectionRegistry();
    const conn1 = createMockConnection();
    const conn2 = createMockConnection();

    await registry.register("sess-1", conn1);
    expect(registry.isAlive("sess-1", conn1)).toBe(true);

    await registry.register("sess-1", conn2);
    expect(registry.isAlive("sess-1", conn1)).toBe(false);
  });

  it("returns false after explicit unregister", async () => {
    const registry = new ConnectionRegistry();
    const conn = createMockConnection();

    await registry.register("sess-1", conn);
    registry.unregister("sess-1", conn);

    expect(registry.isAlive("sess-1", conn)).toBe(false);
  });

  it("returns true after re-registering a previously closed connection", async () => {
    const registry = new ConnectionRegistry();
    const conn = createMockConnection();

    await registry.register("sess-1", conn);
    expect(registry.isAlive("sess-1", conn)).toBe(true);

    // Replace with same connection object (edge case).
    await registry.register("sess-1", conn);
    expect(registry.isAlive("sess-1", conn)).toBe(true);
  });
});
