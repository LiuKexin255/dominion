/**
 * Plugin-face tests for the cordis service entry (specs/054-agent-v2-
 * bugfixes/tasks.md T015): `isDesktopConnected` reads the connection
 * registry through the plugin delegation (specs/054-agent-v2-bugfixes/
 * contracts/agent-api-changes.md §4 — the GetAgent desktop_connected fact
 * source), covering the no-connection / connected / takeover / disconnect
 * paths. Pattern (style/javascript.md Mock convention): a real cordis
 * Context and injected stream doubles — no module interception (saolei
 * plugin test precedent, common/js/dsh-plugins/saolei/src/index.test.ts).
 */

import { Context } from '@deepseek-ai/cordis';
import { describe, expect, it } from 'vitest';

import { DesktopBridgePlugin } from './index.js';
import type { BidiStream } from './wire.js';

const SESSION = 'templates/saolei/sessions/s1';

/** Minimal stream double carrying only what attach/disconnect exercise. */
function fakeStream() {
  const listeners = new Map<string, Array<() => void>>();
  const stream: BidiStream = {
    on(event, listener) {
      const list = listeners.get(event) ?? [];
      list.push(listener as () => void);
      listeners.set(event, list);
      return stream;
    },
    write: () => true,
    end: () => stream,
  };
  return {
    stream,
    emitEnd: () => {
      for (const listener of [...(listeners.get('end') ?? [])]) listener();
    },
  };
}

function makePlugin(): { ctx: Context; plugin: DesktopBridgePlugin } {
  const ctx = new Context();
  const plugin = new DesktopBridgePlugin(ctx);
  return { ctx, plugin };
}

describe('DesktopBridgePlugin.isDesktopConnected', () => {
  it('mounts the service on the context — ctx.desktopBridge serves the same registry', () => {
    // cordis exposes service properties through its reactive proxy, so the
    // mount contract is behavioral, not reference identity: the host's
    // `ctx.desktopBridge.isDesktopConnected` (agent_v2 buildServer wiring)
    // must read the same registry the plugin face writes.
    const { ctx, plugin } = makePlugin();
    plugin.attach(SESSION, fakeStream().stream);
    expect(ctx.desktopBridge.isDesktopConnected(SESSION)).toBe(true);
    expect(ctx.desktopBridge.isDesktopConnected('templates/saolei/sessions/other')).toBe(false);
  });

  it('reports false with no connection for the session', () => {
    const { plugin } = makePlugin();
    expect(plugin.isDesktopConnected(SESSION)).toBe(false);
  });

  it(`reports true once the session's stream is attached`, () => {
    const { plugin } = makePlugin();
    plugin.attach(SESSION, fakeStream().stream);
    expect(plugin.isDesktopConnected(SESSION)).toBe(true);
    expect(plugin.isDesktopConnected('templates/saolei/sessions/other')).toBe(false);
  });

  it(`stays true across a takeover — the superseded stream's late disconnect is a no-op`, () => {
    const { plugin } = makePlugin();
    const first = fakeStream();
    plugin.attach(SESSION, first.stream);

    const second = fakeStream();
    plugin.attach(SESSION, second.stream);
    expect(plugin.isDesktopConnected(SESSION)).toBe(true);

    first.emitEnd();
    expect(plugin.isDesktopConnected(SESSION)).toBe(true);
  });

  it('reports false after the live connection disconnects', () => {
    const { plugin } = makePlugin();
    const connection = fakeStream();
    plugin.attach(SESSION, connection.stream);
    connection.emitEnd();
    expect(plugin.isDesktopConnected(SESSION)).toBe(false);
  });
});
