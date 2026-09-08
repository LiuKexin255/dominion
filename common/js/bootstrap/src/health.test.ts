import * as net from "node:net";
import { describe, it, expect, beforeAll, afterAll } from "vitest";
import { createHealthServer, type HealthService } from "./health.js";

// Real binding on an OS-assigned port (same strategy as Go
// common/gopkg/bootstrap/health_test.go): bazel may run this target
// concurrently with other health tests, so the fixed 38080 would collide.
// Cases run serially within this file.

/** Reserves an OS-assigned port and releases it for the caller to bind. */
function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const probe = net.createServer();
    probe.once("error", reject);
    probe.listen(0, "127.0.0.1", () => {
      const { port } = probe.address() as net.AddressInfo;
      probe.close(() => resolve(port));
    });
  });
}

describe("health endpoint", () => {
  let health: HealthService;
  let port: number;

  beforeAll(async () => {
    port = await freePort();
    health = createHealthServer();
    await health.start(port);
  });

  afterAll(async () => {
    await health.stop(AbortSignal.timeout(5000));
  });

  it("GET /healthz returns 200 with the ok body", async () => {
    const res = await fetch(`http://127.0.0.1:${port}/healthz`);
    expect(res.status).toBe(200);
    expect(await res.text()).toBe("ok\n");
  });

  it("GET / returns 404", async () => {
    const res = await fetch(`http://127.0.0.1:${port}/`);
    expect(res.status).toBe(404);
  });

  it("GET unknown path returns 404", async () => {
    const res = await fetch(`http://127.0.0.1:${port}/unknown`);
    expect(res.status).toBe(404);
  });

  it("GET /healthz/extra returns 404", async () => {
    const res = await fetch(`http://127.0.0.1:${port}/healthz/extra`);
    expect(res.status).toBe(404);
  });
});

describe("stop releases the port", () => {
  it("the port can be bound again after stop", async () => {
    const port = await freePort();
    const first = createHealthServer();
    await first.start(port);
    await first.stop(AbortSignal.timeout(5000));

    const second = createHealthServer();
    await second.start(port);
    await second.stop(AbortSignal.timeout(5000));
  });
});
