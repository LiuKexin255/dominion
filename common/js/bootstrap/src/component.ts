/**
 * Component contract for the bootstrap lifecycle manager.
 *
 * Signatures follow `specs/053-js-bootstrap-migration/contracts/bootstrap-js-api.md §2`.
 */

/**
 * Lifecycle stages. Numeric values match the Go bootstrap
 * (common/gopkg/bootstrap/stage.go) so startup ordering semantics stay
 * identical across languages.
 */
export const Stage = {
  Foundation: 100,
  Client: 200,
  Daemon: 250,
  Server: 300,
} as const;

export type Stage = (typeof Stage)[keyof typeof Stage];

/**
 * A unit managed by the Bootstrap orchestrator. Failures are expressed as
 * promise rejections (throw), never as returned error values.
 */
export interface Component {
  /** Unique name within a Bootstrap instance. */
  readonly name: string;
  /** Startup ordering key (ascending); ties break by name ascending. */
  readonly stage: Stage;
  /** Starts the component. A rejection triggers rollback of started peers. */
  start(signal: AbortSignal): Promise<void>;
  /** Stops the component within the shared shutdown budget signal. */
  stop(signal: AbortSignal): Promise<void>;
}

/**
 * Optional unexpected-exit signal: resolve carries an Error when the
 * component died unexpectedly; resolving with undefined means a normal
 * exit that must not trigger a global shutdown. Implemented by server-like
 * components and daemons (research.md D3/D7 in
 * specs/053-js-bootstrap-migration/research.md).
 */
export interface ExitWatchable {
  readonly exited: Promise<Error | undefined>;
}
