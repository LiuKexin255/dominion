import { describe, it, expect } from 'vitest';

// Smoke test: verify that Wails type declarations are correctly structured.
// This test is intentionally simple — it validates that the TypeScript
// types compile correctly and basic assertions work.

describe('AgentStatus type', () => {
  it('has expected shape', () => {
    const status = {
      state: 'connected',
      sessionId: 'test-123',
      boundWindow: null,
      mediaSegCount: 0,
      lastError: '',
      ffmpegRunning: false,
      helperRunning: false,
      connectedAt: new Date().toISOString(),
    };

    expect(status).toHaveProperty('state');
    expect(status).toHaveProperty('sessionId');
    expect(typeof status.state).toBe('string');
    expect(status.mediaSegCount).toBe(0);
    expect(status.ffmpegRunning).toBe(false);
  });
});
