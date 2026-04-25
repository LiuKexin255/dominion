import { EventEmitter } from 'events';
import { PassThrough } from 'stream';

/**
 * Mock ChildProcess for testing encoder/input helper spawning.
 * Mimics the Node.js ChildProcess interface with controllable streams.
 */
export interface MockChildProcess {
  /** Unique identifier for this mock process. */
  pid: number;
  /** Simulated stdout stream. */
  stdout: PassThrough;
  /** Simulated stderr stream. */
  stderr: PassThrough;
  /** Simulated stdin stream. */
  stdin: PassThrough;
  /** Emits 'close', 'exit', 'error' events. */
  emitter: EventEmitter;
  /** Simulate the process exiting with a given code. */
  exit: (code: number) => void;
  /** Simulate the process emitting an error. */
  emitError: (err: Error) => void;
  /** Emit data on stdout. */
  emitStdout: (data: string) => void;
  /** Emit data on stderr. */
  emitStderr: (data: string) => void;
}

/**
 * Create a mock ChildProcess for testing.
 */
export function createMockChildProcess(): MockChildProcess {
  const emitter = new EventEmitter();
  const stdout = new PassThrough();
  const stderr = new PassThrough();
  const stdin = new PassThrough();

  const pid = Math.floor(Math.random() * 65535) + 1;

  return {
    pid,
    stdout,
    stderr,
    stdin,
    emitter,
    exit(code: number) {
      emitter.emit('close', code);
      emitter.emit('exit', code);
      stdout.end();
      stderr.end();
    },
    emitError(err: Error) {
      emitter.emit('error', err);
    },
    emitStdout(data: string) {
      stdout.write(data);
    },
    emitStderr(data: string) {
      stderr.write(data);
    },
  };
}

/**
 * Mock spawn function that returns a MockChildProcess.
 * Useful for replacing `child_process.spawn` in tests.
 */
export function createMockSpawn() {
  return (_command: string, _args?: readonly string[], _options?: unknown): MockChildProcess => {
    return createMockChildProcess();
  };
}
