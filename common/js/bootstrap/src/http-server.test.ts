import { describe, it, expect, vi } from "vitest";
import * as http from "node:http";
import { createHttpServerComponent } from "./http-server.js";

// Real node:http servers on ephemeral :0 ports (the fixed-port conflict case
// binds an explicit port), mirroring the Go http_test.go strategy.

function makeServer(): http.Server {
  return http.createServer((_req, res) => {
    // Deliberately never end: keeps the connection active for the
    // force-close-on-budget-abort case.
    void res;
  });
}

function neverAborted(): AbortSignal {
  return new AbortController().signal;
}

/** Binds the server through the component and returns the concrete port. */
async function startOnEphemeralPort(name: string): Promise<{ component: ReturnType<typeof createHttpServerComponent>; server: http.Server; port: number }> {
  const server = makeServer();
  const component = createHttpServerComponent(name, server, { port: 0, host: "127.0.0.1" });
  await component.start(neverAborted());
  const address = server.address();
  if (address === null || typeof address === "string") {
    throw new Error("expected a TCP address");
  }
  return { component, server, port: address.port };
}

describe("createHttpServerComponent", () => {
  it("starts listening on the given port", async () => {
    const { component, server, port } = await startOnEphemeralPort("http-ok");
    try {
      expect(server.listening).toBe(true);
      expect(port).toBeGreaterThan(0);
    } finally {
      await component.stop(AbortSignal.timeout(5000));
    }
  });

  it("rejects start when the port is already taken", async () => {
    const first = await startOnEphemeralPort("http-first");
    try {
      const server = makeServer();
      const second = createHttpServerComponent("http-second", server, { port: first.port, host: "127.0.0.1" });
      await expect(second.start(neverAborted())).rejects.toThrow();
      expect(server.listening).toBe(false);
    } finally {
      await first.component.stop(AbortSignal.timeout(5000));
    }
  });

  it("releases the port after stop", async () => {
    const first = await startOnEphemeralPort("http-release");
    const port = first.port;
    await first.component.stop(AbortSignal.timeout(5000));
    expect(first.server.listening).toBe(false);

    const rebind = makeServer();
    const rebindComponent = createHttpServerComponent("http-rebind", rebind, { port, host: "127.0.0.1" });
    await rebindComponent.start(neverAborted());
    expect(rebind.listening).toBe(true);
    await rebindComponent.stop(AbortSignal.timeout(5000));
  });

  it("forces connections closed once the budget signal aborts", async () => {
    const server = makeServer();
    const component = createHttpServerComponent("http-hang", server, { port: 0, host: "127.0.0.1" });
    await component.start(neverAborted());
    const address = server.address();
    if (address === null || typeof address === "string") throw new Error("expected a TCP address");

    // A request whose handler never answers keeps a connection open, so the
    // plain close() drain cannot finish without closeAllConnections().
    const connected = new Promise<void>((resolve) => server.once("connection", resolve));
    const pending = http.request({ host: "127.0.0.1", port: address.port });
    // The forced close resets the connection; swallow the expected error.
    pending.on("error", () => {});
    pending.end();
    await connected;
    // Let the request be parsed so the socket counts as active, not idle.
    await new Promise((resolve) => setTimeout(resolve, 20));

    const realCloseAll = server.closeAllConnections.bind(server);
    const closeAll = vi.fn(() => realCloseAll());
    server.closeAllConnections = closeAll;

    try {
      await component.stop(AbortSignal.timeout(200));
      expect(closeAll).toHaveBeenCalled();
      expect(server.listening).toBe(false);
    } finally {
      pending.destroy();
    }
  });

  it("resolves exited with an error on unexpected close", async () => {
    const { component, server } = await startOnEphemeralPort("http-crash");
    // Close outside the component's stop: the exited promise must report the
    // unexpected exit instead of a clean undefined.
    server.close(() => {});
    await expect(component.exited).resolves.toBeInstanceOf(Error);
  });

  it("resolves exited with undefined after an intended stop", async () => {
    const { component } = await startOnEphemeralPort("http-clean");
    await component.stop(AbortSignal.timeout(5000));
    await expect(component.exited).resolves.toBeUndefined();
  });
});
