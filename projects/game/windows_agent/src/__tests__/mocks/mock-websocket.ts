import { EventEmitter } from 'events';

/**
 * Mock WebSocket server for transport-layer tests.
 * Simulates a simple WebSocket server that can send/receive messages.
 */
export interface MockWebSocketServer {
  /** Underlying EventEmitter for hooking into events. */
  emitter: EventEmitter;
  /** Simulate broadcasting a message to all connected clients. */
  broadcast: (data: string) => void;
  /** Clean up all listeners. */
  close: () => void;
}

/**
 * Mock WebSocket client for transport-layer tests.
 * Mimics the browser/Node WebSocket API.
 */
export interface MockWebSocketClient {
  /** The underlying EventEmitter used internally. */
  emitter: EventEmitter;
  /** Simulate sending a message from client to server. */
  send: (data: string | ArrayBufferLike | Blob | ArrayBufferView) => void;
  /** Simulate closing the client connection. */
  close: (code?: number, reason?: string) => void;
  /** Register a message handler. */
  onMessage: (handler: (data: string) => void) => void;
  /** Simulate an incoming message from the server. */
  receive: (data: string) => void;
}

/**
 * Create a mock WebSocket server for testing.
 */
export function createMockWebSocketServer(): MockWebSocketServer {
  const emitter = new EventEmitter();

  return {
    emitter,
    broadcast(data: string) {
      emitter.emit('message', data);
    },
    close() {
      emitter.removeAllListeners();
    },
  };
}

/**
 * Create a mock WebSocket client for testing.
 */
export function createMockWebSocketClient(): MockWebSocketClient {
  const emitter = new EventEmitter();
  let messageHandler: ((data: string) => void) | null = null;

  return {
    emitter,
    send(_data: string | ArrayBufferLike | Blob | ArrayBufferView) {
      const dataStr = typeof _data === 'string' ? _data : String(_data);
      emitter.emit('send', dataStr);
    },
    close(_code?: number, _reason?: string) {
      emitter.emit('close', _code, _reason);
    },
    onMessage(handler: (data: string) => void) {
      messageHandler = handler;
      emitter.on('message', handler);
    },
    receive(data: string) {
      if (messageHandler) {
        messageHandler(data);
      }
      emitter.emit('message', data);
    },
  };
}
