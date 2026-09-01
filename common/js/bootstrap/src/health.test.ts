import { describe, it, expect, beforeAll, afterAll } from "vitest";
import { createHealthServer, type HealthService } from "./health.js";

// Real binding on the fixed :38080 port (same strategy as Go
// common/gopkg/bootstrap/health_test.go); cases run serially within this file.
const BASE = "http://127.0.0.1:38080";

describe("health endpoint", () => {
  let health: HealthService;

  beforeAll(async () => {
    health = createHealthServer();
    await health.start();
  });

  afterAll(async () => {
    await health.stop(AbortSignal.timeout(5000));
  });

  it("GET /healthz returns 200 with the ok body", async () => {
    const res = await fetch(`${BASE}/healthz`);
    expect(res.status).toBe(200);
    expect(await res.text()).toBe("ok\n");
  });

  it("GET / returns 404", async () => {
    const res = await fetch(`${BASE}/`);
    expect(res.status).toBe(404);
  });

  it("GET unknown path returns 404", async () => {
    const res = await fetch(`${BASE}/unknown`);
    expect(res.status).toBe(404);
  });

  it("GET /healthz/extra returns 404", async () => {
    const res = await fetch(`${BASE}/healthz/extra`);
    expect(res.status).toBe(404);
  });
});

describe("stop releases the port", () => {
  it("the fixed port can be bound again after stop", async () => {
    const first = createHealthServer();
    await first.start();
    await first.stop(AbortSignal.timeout(5000));

    const second = createHealthServer();
    await second.start();
    await second.stop(AbortSignal.timeout(5000));
  });
});
