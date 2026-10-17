/**
 * Channel policy + cache tests (specs/074-dsh-grpc-runtime/tasks.md T002).
 *
 * The policy builders are pure functions of the CA path and `TLS_SERVER_NAME`,
 * so the deployment branch (mTLS vs insecure) is asserted by injecting a
 * fixture path instead of intercepting `node:fs` (DI, no `vi.mock` —
 * style/javascript.md §测试). Cache sharing uses real grpc-js channels over a
 * local target: channel construction does not connect, and the fan-out to
 * consumers only happens through the `channelOverride` seam.
 */

import { afterEach, describe, expect, it, vi } from "vitest";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

import {
  ChannelPool,
  RECONNECT_OPTIONS,
  ROUND_ROBIN_SERVICE_CONFIG,
  TLS_CA_CERT,
  buildChannelOptions,
  buildClientCredentials,
} from "./channel.js";

const MISSING_CA = path.join(os.tmpdir(), "grpc-runtime-missing-ca", "ca.crt");

const ORIGINAL_TLS_SERVER_NAME = process.env.TLS_SERVER_NAME;

/** A private per-test directory holding a placeholder CA file. */
function makeCaFixture(): { dir: string; caPath: string } {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "grpc-runtime-ca-"));
  const caPath = path.join(dir, "ca.crt");
  fs.writeFileSync(caPath, "test-ca-fixture");
  return { dir, caPath };
}

function removeDir(dir: string): void {
  fs.rmSync(dir, { recursive: true, force: true });
}

afterEach(() => {
  if (ORIGINAL_TLS_SERVER_NAME === undefined) {
    delete process.env.TLS_SERVER_NAME;
  } else {
    process.env.TLS_SERVER_NAME = ORIGINAL_TLS_SERVER_NAME;
  }
});

describe("channel policy", () => {
  it("routes with round_robin and the production reconnect backoff", () => {
    const options = buildChannelOptions(MISSING_CA);
    expect(JSON.parse(options["grpc.service_config"] as string)).toEqual({
      loadBalancingConfig: [{ round_robin: {} }],
    });
    expect(options["grpc.service_config"]).toBe(ROUND_ROBIN_SERVICE_CONFIG);
    expect(options["grpc.initial_reconnect_backoff_ms"]).toBe(1_000);
    expect(options["grpc.max_reconnect_backoff_ms"]).toBe(15_000);
    expect(RECONNECT_OPTIONS["grpc.initial_reconnect_backoff_ms"]).toBe(1_000);
    expect(RECONNECT_OPTIONS["grpc.max_reconnect_backoff_ms"]).toBe(15_000);
  });

  it("never sets app-level keepalive options", () => {
    const options = buildChannelOptions(MISSING_CA);
    // Deliberate policy: app-level keepalive PINGs are GOAWAY'd by the unary
    // server's default enforcement (contracts/grpc-runtime.md §3).
    expect(options["grpc.keepalive_time_ms"]).toBeUndefined();
    expect(options["grpc.keepalive_timeout_ms"]).toBeUndefined();
    expect(options["grpc.keepalive_permit_without_calls"]).toBeUndefined();
  });

  it("adds ssl_target_name_override only when the CA exists", () => {
    process.env.TLS_SERVER_NAME = "memory.dominion.test";

    // CA absent → insecure (local dev): no TLS override.
    expect(
      buildChannelOptions(MISSING_CA)["grpc.ssl_target_name_override"],
    ).toBeUndefined();

    // CA present → mTLS: the target-name override applies.
    const { dir, caPath } = makeCaFixture();
    try {
      expect(
        buildChannelOptions(caPath)["grpc.ssl_target_name_override"],
      ).toBe("memory.dominion.test");
    } finally {
      removeDir(dir);
    }
  });

  it("ignores TLS_SERVER_NAME when unset", () => {
    delete process.env.TLS_SERVER_NAME;
    const { dir, caPath } = makeCaFixture();
    try {
      expect(
        buildChannelOptions(caPath)["grpc.ssl_target_name_override"],
      ).toBeUndefined();
    } finally {
      removeDir(dir);
    }
  });

  it("selects insecure credentials when the CA is absent", () => {
    // A missing CA is the local-dev path: createInsecure, no fs read.
    expect(buildClientCredentials(MISSING_CA).constructor.name).toBe(
      "InsecureChannelCredentialsImpl",
    );
  });

  it("selects TLS credentials (createSsl) when the CA exists", () => {
    const { dir, caPath } = makeCaFixture();
    try {
      // Exercises the readFileSync + createSsl branch.
      const secure = buildClientCredentials(caPath);
      expect(secure.constructor.name).toBe("SecureChannelCredentialsImpl");
      expect(secure).not.toBe(buildClientCredentials(MISSING_CA));
    } finally {
      removeDir(dir);
    }
  });

  it("keeps the production CA path as the default", () => {
    expect(TLS_CA_CERT).toBe("/etc/tls/ca.crt");
  });
});

describe("ChannelPool", () => {
  it("shares one channel per target and keeps targets separate", () => {
    const pool = new ChannelPool();
    try {
      const first = pool.get("127.0.0.1:50051");
      const again = pool.get("127.0.0.1:50051");
      const other = pool.get("127.0.0.1:50052");

      expect(again).toBe(first);
      expect(other).not.toBe(first);
      expect(pool.size).toBe(2);
    } finally {
      pool.closeAll();
    }
  });

  it("closes every cached channel and forgets them on closeAll", () => {
    const pool = new ChannelPool();
    const channel = pool.get("127.0.0.1:50051");
    const close = vi.spyOn(channel, "close");

    pool.closeAll();

    expect(close).toHaveBeenCalledTimes(1);
    expect(pool.size).toBe(0);
    // A subsequent get builds a fresh channel, not the closed instance.
    expect(pool.get("127.0.0.1:50051")).not.toBe(channel);
    pool.closeAll();
  });
});
