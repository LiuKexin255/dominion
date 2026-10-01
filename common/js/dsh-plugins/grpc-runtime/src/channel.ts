/**
 * Connection policy for the composition-level gRPC runtime: credentials,
 * channel options, and the per-target channel cache.
 *
 * The policy is migrated verbatim from
 * `common/js/dsh-plugins/memory-service/src/client.ts:120-172` (specs/
 * 074-dsh-grpc-runtime/research.md D3 "必须随代码迁移的策略" table): the
 * deployment-scoped constants are identical for every consumer of the
 * runtime, so they live here once instead of per host row. Why-comments ride
 * with the code (constitution principle I; specs/
 * 074-dsh-grpc-runtime/contracts/grpc-runtime.md §3).
 */

import * as fs from "node:fs";

import * as grpc from "@grpc/grpc-js";

/** Deployment CA path: presence flips the runtime from insecure to mTLS. */
export const TLS_CA_CERT = "/etc/tls/ca.crt";

/**
 * Build the TLS channel credentials for dominion gRPC services: TLS with the
 * deployment CA when present, insecure otherwise (local dev). Deployed
 * clusters materialize `/etc/tls/ca.crt` as part of the unified mTLS layout
 * (specs/074-dsh-grpc-runtime/contracts/grpc-runtime.md §3).
 *
 * @param caCertPath CA path override (tests inject a fixture path; production
 *   uses {@link TLS_CA_CERT}).
 */
export function buildClientCredentials(
  caCertPath: string = TLS_CA_CERT,
): grpc.ChannelCredentials {
  if (!fs.existsSync(caCertPath)) {
    return grpc.credentials.createInsecure();
  }

  const rootCert = fs.readFileSync(caCertPath);
  return grpc.credentials.createSsl(rootCert);
}

// grpc-js defaults to pick_first, which pins a single connection; when that
// backend pod restarts the call hangs in "Waiting for LB pick" until
// reconnect. round_robin (matching grpc-go's ClientDefault) connects to every
// resolved endpoint, so a rolling-upgrade pod swap routes around the
// terminating pod.
export const ROUND_ROBIN_SERVICE_CONFIG = JSON.stringify({
  loadBalancingConfig: [{ round_robin: {} }],
});

// Deliberately NO app-level keepalive PINGs on this unary client — this
// mirrors grpc-go's `ClientDefault()` (common/gopkg/grpc/default.go): the Go
// side reserves HTTP/2 keepalive for long-lived streams, paired with
// server-side enforcement relaxation. The unary memory server runs grpc-go's
// DEFAULT enforcement policy (MinTime=5min, PermitWithoutStream=false), so a
// keepalive-enabled client's idle PINGs would be GOAWAY'd as "excess pings"
// and the connection torn down repeatedly
// (specs/074-dsh-grpc-runtime/contracts/grpc-runtime.md §3).
//
// max_reconnect_backoff_ms: cap the subchannel retry interval (grpc-js
// default max backoff is 120s; 15s bounds how long a dead subchannel is
// retried before giving up the connection).
export const RECONNECT_OPTIONS: grpc.ChannelOptions = {
  "grpc.initial_reconnect_backoff_ms": 1_000,
  "grpc.max_reconnect_backoff_ms": 15_000,
};

/**
 * Channel options used by the real construction path (and asserted by tests).
 *
 * @param caCertPath CA path override used to decide the
 *   `grpc.ssl_target_name_override` wiring (tests inject a fixture path;
 *   production uses {@link TLS_CA_CERT}).
 */
export function buildChannelOptions(
  caCertPath: string = TLS_CA_CERT,
): grpc.ChannelOptions {
  const options: grpc.ChannelOptions = {
    ...RECONNECT_OPTIONS,
    "grpc.service_config": ROUND_ROBIN_SERVICE_CONFIG,
  };
  const serverName = process.env.TLS_SERVER_NAME;
  if (serverName && fs.existsSync(caCertPath)) {
    options["grpc.ssl_target_name_override"] = serverName;
  }
  return options;
}

/**
 * Per-target channel cache shared by every service client the runtime builds
 * (specs/074-dsh-grpc-runtime/contracts/grpc-runtime.md §2 O2): grpc-js creates
 * one `Channel` per `Client` (grpc/grpc-node#158, #2896 — one client per
 * service per process; repeatedly creating clients leaks channelz memory), so
 * multi-service clients over the same endpoint MUST share one channel through
 * the official `channelOverride` seam
 * (https://github.com/grpc/grpc-node/issues/2214).
 *
 * The cache key is the target alone: the credentials and channel policy are
 * module constants of this runtime. If a future consumer needs a different
 * credentials/channel policy for the same target, this key MUST be extended
 * with that policy (contracts/grpc-runtime.md §2 O2).
 */
export class ChannelPool {
  private readonly channels = new Map<string, grpc.Channel>();

  /** Return the shared channel for `target`, creating it on first use. */
  get(target: string): grpc.Channel {
    const existing = this.channels.get(target);
    if (existing !== undefined) {
      return existing;
    }
    const created = new grpc.Channel(
      target,
      buildClientCredentials(),
      buildChannelOptions(),
    );
    this.channels.set(target, created);
    return created;
  }

  /** Close and forget every cached channel (composition dispose). */
  closeAll(): void {
    for (const channel of this.channels.values()) {
      channel.close();
    }
    this.channels.clear();
  }

  /** Number of cached channels (diagnostics/tests). */
  get size(): number {
    return this.channels.size;
  }
}
