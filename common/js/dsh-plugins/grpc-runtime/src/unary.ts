/**
 * Unary invocation helper for the composition-level gRPC runtime.
 *
 * Strategy: the shared unary policy (Metadata waitForReady, a 10s default
 * deadline, a pre-call connection probe, a post-failure channel-state log),
 * plus the optional `deadlineMs` parameter (contracts/grpc-runtime.md §2 O3).
 * The runtime is domain-agnostic and label-driven: the caller supplies the
 * label (typically its own service name), so no service-specific wording lives
 * here.
 */

import * as grpc from "@grpc/grpc-js";
import { error, info } from "@dominion/common-js-logs";

/**
 * Callback-style unary invocation shape shared by every service client built
 * by the runtime: request in, callback out, `{deadline}` call options.
 */
export type UnaryInvoke<T> = (
  client: grpc.Client,
  metadata: grpc.Metadata,
  options: { deadline: Date },
  callback: (err: grpc.ServiceError | null, response?: T) => void,
) => void;

/**
 * Shared callback-style unary RPC invocation (request +
 * Metadata({waitForReady}) + deadline). A channel that dropped is probed first
 * so the call does not queue forever in "Waiting for LB pick"; a failure is
 * logged with the post-failure channel state. Every service client the runtime
 * builds shares this one policy through the `label` (they hit the same
 * dominion target).
 *
 * @param deadlineMs Deadline budget in milliseconds; defaults to 10_000 (the
 *   deployment-wide unary budget).
 */
export function callUnary<T>(
  client: grpc.Client,
  label: string,
  invoke: UnaryInvoke<T>,
  deadlineMs = 10_000,
): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const deadline = new Date();
    deadline.setTime(deadline.getTime() + deadlineMs);
    probeChannel(client, label);
    invoke(
      client,
      new grpc.Metadata({ waitForReady: true }),
      { deadline },
      (err: grpc.ServiceError | null, response?: T) => {
        if (err) {
          error(`${label} failed`, {
            error: err.message,
            code: String(err.code),
            channelState: probeChannel(client, `${label} (after failure)`),
          });
          reject(err);
          return;
        }
        resolve(response as T);
      },
    );
  });
}

/**
 * Probe a grpc-js channel's connectivity state and nudge it to connect
 * (`getConnectivityState(true)` forces IDLE→CONNECTING): a call on an idle
 * channel then actively tries to reconnect instead of silently queueing until
 * the deadline. Logs the state for diagnosis and returns the state name.
 *
 * @param client The gRPC client whose channel to probe.
 * @param label  Log label identifying the call site.
 * @returns The connectivity state name, or a descriptive string when the
 *   channel is unavailable (injected test client without a channel).
 */
export function probeChannel(client: grpc.Client, label: string): string {
  let state = "no-channel";
  try {
    const channel = (
      client as unknown as { getChannel?: () => grpc.Channel }
    ).getChannel?.();
    if (channel) {
      const value = channel.getConnectivityState(true);
      state = grpc.connectivityState[value] ?? String(value);
    }
  } catch (err) {
    state = `error:${err instanceof Error ? err.message : String(err)}`;
  }
  info(`${label}: channel state`, { state });
  return state;
}
