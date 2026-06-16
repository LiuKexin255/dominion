/**
 * connection-registry.ts — Per-session connection registry for gRPC streams.
 *
 * Enforces single-connection-per-session: registering a new connection for an
 * active session kicks (closes) the old one.  All register calls for the same
 * session are serialized via a per-session promise-chain mutex (same pattern
 * as handler.ts) so concurrent registrations always converge on exactly one
 * surviving connection.
 */

// ---------------------------------------------------------------------------
// Connection interface
// ---------------------------------------------------------------------------

/** Lightweight abstraction over a gRPC stream duplex. */
export interface Connection {
  /** Fire-and-forget close. Called during kick — never awaited. */
  close(): void;
}

/** Internal per-session state tracked by the registry. */
interface SessionEntry {
  connection: Connection;
  alive: boolean;
}

// ---------------------------------------------------------------------------
// ConnectionRegistry
// ---------------------------------------------------------------------------

export class ConnectionRegistry {
  /** Session → connection + liveness. */
  private sessions = new Map<string, SessionEntry>();

  /** Per-session FIFO mutex (promise chain). */
  private mutexes = new Map<string, Promise<void>>();

  // -----------------------------------------------------------------------
  // Mutex helpers (same pattern as handler.ts)
  // -----------------------------------------------------------------------

  private async acquireMutex(sessionId: string): Promise<void> {
    const prev = this.mutexes.get(sessionId) ?? Promise.resolve();
    let release!: () => void;
    const next = new Promise<void>((r) => {
      release = r;
    });
    this.mutexes.set(sessionId, prev.then(() => next));
    await prev;
    (this.mutexes as any)[`_release_${sessionId}`] = release;
  }

  private releaseMutex(sessionId: string): void {
    const release = (this.mutexes as any)[`_release_${sessionId}`];
    if (release) release();
  }

  // -----------------------------------------------------------------------
  // Public API
  // -----------------------------------------------------------------------

  /**
   * Register a connection for a session.
   *
   * If a connection already exists and is alive, it is closed (fire-and-forget)
   * and marked dead before the new connection is registered.
   *
   * Concurrent register calls are serialized per session, guaranteeing that
   * after all calls settle exactly one connection survives.
   */
  async register(sessionId: string, connection: Connection): Promise<void> {
    await this.acquireMutex(sessionId);
    try {
      const existing = this.sessions.get(sessionId);
      if (existing && existing.alive) {
        // Kick the old connection — fire-and-forget, never await.
        existing.alive = false;
        existing.connection.close();
      }
      this.sessions.set(sessionId, { connection, alive: true });
    } finally {
      this.releaseMutex(sessionId);
    }
  }

  /**
   * Unregister a connection for a session.
   *
   * Only removes the entry if `connection` matches the currently registered
   * one.  If the connection has already been replaced by a newer registration,
   * this is a no-op.
   */
  unregister(sessionId: string, connection: Connection): void {
    const entry = this.sessions.get(sessionId);
    if (entry && entry.connection === connection) {
      this.sessions.delete(sessionId);
    }
  }

  /**
   * Return true iff `connection` is the currently registered connection for
   * `sessionId` and is still marked alive.
   */
  isAlive(sessionId: string, connection: Connection): boolean {
    const entry = this.sessions.get(sessionId);
    return entry !== undefined && entry.alive && entry.connection === connection;
  }
}
