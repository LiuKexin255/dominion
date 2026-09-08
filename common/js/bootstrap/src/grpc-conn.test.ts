import { describe, it, expect, vi } from "vitest";
import type { Client as GrpcClient } from "@grpc/grpc-js";
import { createGrpcConnComponent } from "./grpc-conn.js";
import { Stage } from "./component.js";

// Structured fake of the grpc-js Client surface used by the adapter (DI
// injection, no real grpc instance, no runtime grpc-js load).

function fakeClient() {
  const close = vi.fn();
  return { client: { close } as unknown as GrpcClient, close };
}

function neverAborted(): AbortSignal {
  return new AbortController().signal;
}

describe("createGrpcConnComponent", () => {
  it("is a Client-stage component without exited", () => {
    const fake = fakeClient();
    const c = createGrpcConnComponent("conn", fake.client);
    expect(c.stage).toBe(Stage.Client);
    expect("exited" in c).toBe(false);
  });

  it("start is a no-op and stop closes the client", async () => {
    const fake = fakeClient();
    const c = createGrpcConnComponent("conn", fake.client);
    await expect(c.start(neverAborted())).resolves.toBeUndefined();
    expect(fake.close).not.toHaveBeenCalled();
    await expect(c.stop(neverAborted())).resolves.toBeUndefined();
    expect(fake.close).toHaveBeenCalledOnce();
  });
});
