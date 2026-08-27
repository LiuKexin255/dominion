import { describe, expect, it, vi, afterEach } from "vitest";
import { bootDsh, cordisConfigPath, FAKE_LLM_TARGET } from "./dsh.js";
import type { DshBootDeps, DshContext } from "./dsh.js";
import type { EndpointResolver } from "@dominion/common-js-resolver";

/**
 * Fail-loud unit tests for the composition boot path (specs/047-dsh-chat-demo/spec.md FR-009
 * and US3 acceptance scenario 3: enabling a plugin outside the materialized closure
 * must fail the process with diagnostics, never degrade silently).
 *
 * `boot` and the resolver are injected as `vi.fn()` doubles through the
 * DshBootDeps seam (style/javascript.md Mock convention); `process.exit` is
 * spied so the failure path can be asserted without killing the test runner.
 */

function fakeResolver(endpoints: string[]): EndpointResolver {
  return { resolve: vi.fn(async () => endpoints) };
}

function fakeBoot(ctx: DshContext) {
  // Optional params typed as supertypes of boot's real signature so the
  // mock stays assignable to `typeof boot` (contravariant parameters).
  return vi.fn(
    async (
      _binName: string,
      _configPath: string,
      _patches?: unknown,
      _prepare?: unknown,
      _anchor?: string,
    ) => ctx,
  );
}

afterEach(() => {
  vi.restoreAllMocks();
  delete process.env.FAKE_LLM_BASE_URL;
});

describe("bootDsh", () => {
  it("injects the resolved fake-llm base URL and returns the booted context", async () => {
    const ctx = { marker: "ctx" } as unknown as DshContext;
    const boot = fakeBoot(ctx);
    const resolve = vi.fn(async () => ["10.0.0.9:8080"]);
    const exit = vi
      .spyOn(process, "exit")
      .mockImplementation((() => undefined) as never);

    const result = await bootDsh({ boot, resolver: { resolve } });

    expect(result).toBe(ctx);
    expect(resolve).toHaveBeenCalledWith(FAKE_LLM_TARGET);
    expect(process.env.FAKE_LLM_BASE_URL).toBe("http://10.0.0.9:8080/v1");
    expect(boot).toHaveBeenCalledTimes(1);
    const args = boot.mock.calls[0] as unknown[];
    expect(args[0]).toBe("dsh-demo-agent");
    expect(args[1]).toBe(cordisConfigPath());
    // The bare-module anchor pins plugin resolution at this module.
    expect(String(args[4])).toContain("dsh.ts");
    expect(exit).not.toHaveBeenCalled();
  });

  it("fails loud with diagnostics and exit(1) when boot throws", async () => {
    const consoleError = vi
      .spyOn(console, "error")
      .mockImplementation(() => {});
    const boot = vi.fn(async () => {
      throw new Error("cordis.yml row 3: peer dependency missing");
    }) as unknown as DshBootDeps["boot"];
    const exit = vi
      .spyOn(process, "exit")
      .mockImplementation((() => undefined) as never);

    await bootDsh({ boot, resolver: fakeResolver(["10.0.0.9:8080"]) });

    expect(boot).toHaveBeenCalledTimes(1);
    expect(consoleError).toHaveBeenCalledWith(
      expect.stringContaining("boot failed (fail-loud)"),
    );
    expect(consoleError).toHaveBeenCalledWith(
      expect.stringContaining("peer dependency missing"),
    );
    expect(exit).toHaveBeenCalledWith(1);
  });

  it("fails loud when cordis.yml enables a plugin outside the materialized closure", async () => {
    // US3 acceptance scenario 3: the Loader's fail-loud guard surfaces as a
    // boot rejection naming the unresolved plugin; the process must exit(1)
    // with diagnostics instead of silently skipping the row.
    const consoleError = vi
      .spyOn(console, "error")
      .mockImplementation(() => {});
    const boot = vi.fn(async () => {
      throw new Error(
        "cannot resolve plugin '@deepseek-ai/dsh-tool-bash': not installed under the bare-module anchor",
      );
    }) as unknown as DshBootDeps["boot"];
    const exit = vi
      .spyOn(process, "exit")
      .mockImplementation((() => undefined) as never);

    await bootDsh({ boot, resolver: fakeResolver(["10.0.0.9:8080"]) });

    expect(consoleError).toHaveBeenCalledWith(
      expect.stringContaining("dsh-tool-bash"),
    );
    expect(exit).toHaveBeenCalledWith(1);
  });

  it("fails loud when the resolver returns no endpoints", async () => {
    const consoleError = vi
      .spyOn(console, "error")
      .mockImplementation(() => {});
    const boot = fakeBoot({} as DshContext);
    const exit = vi
      .spyOn(process, "exit")
      .mockImplementation((() => undefined) as never);

    await bootDsh({ boot, resolver: fakeResolver([]) });

    expect(boot).not.toHaveBeenCalled();
    expect(consoleError).toHaveBeenCalledWith(
      expect.stringContaining("no endpoints"),
    );
    expect(exit).toHaveBeenCalledWith(1);
  });
});
