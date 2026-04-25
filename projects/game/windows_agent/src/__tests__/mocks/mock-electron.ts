import { EventEmitter } from 'events';

/**
 * Mock Stream object returned by desktopCapturer.getSources.
 */
export interface MockDesktopCapturerSource {
  name: string;
  id: string;
  display_id: string;
  appIcon: null;
}

/**
 * Mock desktopCapturer API.
 */
export interface MockDesktopCapturer {
  getSources: (options: { types: string[]; thumbnailSize?: { width: number; height: number } }) =>
    Promise<MockDesktopCapturerSource[]>;
}

/**
 * Mock contextBridge API.
 */
export interface MockContextBridge {
  exposeInMainWorld: (key: string, api: Record<string, unknown>) => void;
  /** Track which APIs have been exposed for test assertions. */
  _exposed: Map<string, Record<string, unknown>>;
}

/**
 * Mock ipcMain (main process).
 */
export interface MockIpcMain {
  handle: (channel: string, listener: (event: unknown, ...args: unknown[]) => unknown) => void;
  on: (channel: string, listener: (event: unknown, ...args: unknown[]) => void) => void;
  emitter: EventEmitter;
}

/**
 * Mock ipcRenderer (renderer process).
 */
export interface MockIpcRenderer {
  invoke: (channel: string, ...args: unknown[]) => Promise<unknown>;
  on: (channel: string, listener: (event: unknown, ...args: unknown[]) => void) => void;
  send: (channel: string, ...args: unknown[]) => void;
  emitter: EventEmitter;
}

/**
 * Create a mock desktopCapturer for testing.
 */
export function createMockDesktopCapturer(): MockDesktopCapturer {
  return {
    async getSources(_options: { types: string[]; thumbnailSize?: { width: number; height: number } }) {
      return [
        { name: 'Entire Screen', id: 'screen:0:0', display_id: '0', appIcon: null },
        { name: 'Window: Test', id: 'window:12345', display_id: '', appIcon: null },
      ];
    },
  };
}

/**
 * Create a mock contextBridge for testing.
 */
export function createMockContextBridge(): MockContextBridge {
  const exposed = new Map<string, Record<string, unknown>>();

  return {
    exposeInMainWorld(key: string, api: Record<string, unknown>) {
      exposed.set(key, api);
    },
    get _exposed() {
      return exposed;
    },
  };
}

/**
 * Create a mock ipcMain for testing.
 */
export function createMockIpcMain(): MockIpcMain {
  const emitter = new EventEmitter();
  const handlers = new Map<string, (event: unknown, ...args: unknown[]) => unknown>();

  return {
    handle(channel: string, listener: (event: unknown, ...args: unknown[]) => unknown) {
      handlers.set(channel, listener);
    },
    on(channel: string, listener: (event: unknown, ...args: unknown[]) => void) {
      emitter.on(channel, listener);
    },
    emitter,
    /** Internal method to simulate an IPC invoke from the renderer. */
    _handleInvoke(channel: string, ...args: unknown[]): Promise<unknown> {
      const handler = handlers.get(channel);
      if (!handler) {
        return Promise.reject(new Error(`No handler for channel: ${channel}`));
      }
      return Promise.resolve(handler({}, ...args));
    },
  };
}

/**
 * Create a mock ipcRenderer for testing.
 */
export function createMockIpcRenderer(): MockIpcRenderer {
  const emitter = new EventEmitter();

  return {
    async invoke(channel: string, ...args: unknown[]) {
      emitter.emit('invoke', channel, ...args);
      return undefined;
    },
    on(channel: string, listener: (event: unknown, ...args: unknown[]) => void) {
      emitter.on(channel, listener);
    },
    send(channel: string, ...args: unknown[]) {
      emitter.emit(channel, {}, ...args);
    },
    emitter,
  };
}

/**
 * Create a complete mock Electron module for testing.
 * Assembles all individual mocks into one object mirroring the `electron` module.
 */
export function createMockElectron() {
  return {
    desktopCapturer: createMockDesktopCapturer(),
    contextBridge: createMockContextBridge(),
    ipcMain: createMockIpcMain(),
    ipcRenderer: createMockIpcRenderer(),
  };
}
