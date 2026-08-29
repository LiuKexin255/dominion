import { afterEach, describe, expect, it, vi } from "vitest";
import { bootDsh, cordisConfigPath, GLM_DEFAULT_BASE_URL, GLM_SECRET_FILE } from "./dsh.js";
import type { DshBootDeps, DshContext } from "./dsh.js";
import type { EndpointResolver } from "@dominion/common-js-resolver";

/**
 * Fail-loud unit tests for the composition boot path
 * (specs/049-agent-v2-dsh-init/research.md D9): endpoint precedence
 * (GLM_BASE_URL > GLM_LLM_TARGET resolved > default), secret-file token
 * injection with zero token leakage in diagnostics (SC-004), and the
 * boot(binName, configPath, undefined, undefined, import.meta.url) call
 * shape.
 *
 * `boot`, the resolver, and the secret reader are injected as `vi.fn()`
 * doubles through the DshBootDeps seam; `process.exit` is spied so the
 * failure path can be asserted without killing the test runner
 * (style/javascript.md Mock convention).
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

const TOKEN = "glmtoken-do-not-leak-9f8e7d6c";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("bootDsh", () => {
  it("prefers an already-set GLM_BASE_URL without contacting the resolver", async () => {
    const ctx = { marker: "ctx" } as unknown as DshContext;
    const boot = fakeBoot(ctx);
    const resolve = vi.fn(async () => ["10.0.0.9:8080"]);
    const exit = vi.spyOn(process, "exit").mockImplementation((() => undefined) as never);
    const env: Record<string, string | undefined> = {
      GLM_BASE_URL: "http://fake-llm:8080/v1",
      GLM_LLM_TARGET: "dominion:///game/fake-llm:8080",
    };

    const result = await bootDsh({
      boot,
      resolver: { resolve },
      env,
      secretDir: "/tmp/secret",
      readSecretFile: () => TOKEN,
    });

    expect(result).toBe(ctx);
    expect(resolve).not.toHaveBeenCalled();
    expect(env.GLM_BASE_URL).toBe("http://fake-llm:8080/v1");
    expect(exit).not.toHaveBeenCalled();
  });

  it("resolves GLM_LLM_TARGET through Dominion discovery and appends the /v1 path", async () => {
    const ctx = { marker: "ctx" } as unknown as DshContext;
    const boot = fakeBoot(ctx);
    const resolve = vi.fn(async () => ["10.0.0.9:8080"]);
    const exit = vi.spyOn(process, "exit").mockImplementation((() => undefined) as never);
    const env: Record<string, string | undefined> = {
      GLM_LLM_TARGET: "dominion:///game/fake-llm:8080",
    };

    await bootDsh({
      boot,
      resolver: { resolve },
      env,
      secretDir: "/tmp/secret",
      readSecretFile: () => TOKEN,
    });

    expect(resolve).toHaveBeenCalledWith("dominion:///game/fake-llm:8080");
    expect(env.GLM_BASE_URL).toBe("http://10.0.0.9:8080/v1");
  });

  it("falls back to the production GLM endpoint when neither env override is set", async () => {
    const ctx = { marker: "ctx" } as unknown as DshContext;
    const boot = fakeBoot(ctx);
    const resolve = vi.fn(async () => {
      throw new Error("resolver must not be contacted without GLM_LLM_TARGET");
    });
    const exit = vi.spyOn(process, "exit").mockImplementation((() => undefined) as never);
    const env: Record<string, string | undefined> = {};

    await bootDsh({
      boot,
      resolver: { resolve },
      env,
      secretDir: "/tmp/secret",
      readSecretFile: () => TOKEN,
    });

    expect(env.GLM_BASE_URL).toBe(GLM_DEFAULT_BASE_URL);
  });

  it("injects the trimmed token from the secret file as GLM_API_KEY", async () => {
    const ctx = { marker: "ctx" } as unknown as DshContext;
    const boot = fakeBoot(ctx);
    const exit = vi.spyOn(process, "exit").mockImplementation((() => undefined) as never);
    const env: Record<string, string | undefined> = {};
    const readSecretFile = vi.fn(() => `  ${TOKEN}\n`);

    await bootDsh({ boot, env, secretDir: "/tmp/secret", readSecretFile });

    expect(readSecretFile).toHaveBeenCalledWith(`/tmp/secret/${GLM_SECRET_FILE}`);
    expect(env.GLM_API_KEY).toBe(TOKEN);
    expect(exit).not.toHaveBeenCalled();
  });

  it("uses $DOMINION_SECRET_DIR for the token file lookup", async () => {
    const ctx = { marker: "ctx" } as unknown as DshContext;
    const boot = fakeBoot(ctx);
    const exit = vi.spyOn(process, "exit").mockImplementation((() => undefined) as never);
    const readSecretFile = vi.fn(() => TOKEN);

    await bootDsh({
      boot,
      env: { DOMINION_SECRET_DIR: "/mnt/dominion/secret" },
      readSecretFile,
    });

    expect(readSecretFile).toHaveBeenCalledWith(`/mnt/dominion/secret/${GLM_SECRET_FILE}`);
  });

  it("fails loud with diagnostics and exit(1) when the token file is missing, leaking no token content", async () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
    const boot = fakeBoot({} as DshContext);
    const exit = vi.spyOn(process, "exit").mockImplementation((() => undefined) as never);
    const readSecretFile = vi.fn(() => {
      throw new Error(`ENOENT: no such file or directory, open '/mnt/dominion/secret/${GLM_SECRET_FILE}'`);
    });

    await bootDsh({ boot, env: {}, readSecretFile });

    expect(boot).not.toHaveBeenCalled();
    expect(exit).toHaveBeenCalledWith(1);
    const logged = consoleError.mock.calls.map((call) => call.join(" ")).join("\n");
    expect(logged).toContain("fail-loud");
    expect(logged).toContain(GLM_SECRET_FILE);
    expect(logged).not.toContain(TOKEN);
  });

  it("fails loud when the token file is empty, leaking no token content", async () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
    const boot = fakeBoot({} as DshContext);
    const exit = vi.spyOn(process, "exit").mockImplementation((() => undefined) as never);

    await bootDsh({ boot, env: {}, secretDir: "/tmp/secret", readSecretFile: () => "   " });

    expect(boot).not.toHaveBeenCalled();
    expect(exit).toHaveBeenCalledWith(1);
    const logged = consoleError.mock.calls.map((call) => call.join(" ")).join("\n");
    expect(logged).toContain("absent or empty");
  });

  it("fails loud when the resolver returns no endpoints for GLM_LLM_TARGET", async () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
    const boot = fakeBoot({} as DshContext);
    const exit = vi.spyOn(process, "exit").mockImplementation((() => undefined) as never);

    await bootDsh({
      boot,
      resolver: fakeResolver([]),
      env: { GLM_LLM_TARGET: "dominion:///game/fake-llm:8080" },
      secretDir: "/tmp/secret",
      readSecretFile: () => TOKEN,
    });

    expect(boot).not.toHaveBeenCalled();
    expect(consoleError).toHaveBeenCalledWith(expect.stringContaining("no endpoints"));
    expect(exit).toHaveBeenCalledWith(1);
  });

  it("fails loud with diagnostics and exit(1) when boot throws", async () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
    const boot = vi.fn(async () => {
      throw new Error("cordis.yml row 2: peer dependency missing");
    }) as unknown as DshBootDeps["boot"];
    const exit = vi.spyOn(process, "exit").mockImplementation((() => undefined) as never);

    await bootDsh({ boot, env: {}, secretDir: "/tmp/secret", readSecretFile: () => TOKEN });

    expect(boot).toHaveBeenCalledTimes(1);
    expect(consoleError).toHaveBeenCalledWith(expect.stringContaining("boot failed (fail-loud)"));
    expect(consoleError).toHaveBeenCalledWith(expect.stringContaining("peer dependency missing"));
    expect(exit).toHaveBeenCalledWith(1);
  });

  it("passes the boot call shape (bin name, cordis.yml path, bare-module anchor)", async () => {
    const ctx = { marker: "ctx" } as unknown as DshContext;
    const boot = fakeBoot(ctx);
    const exit = vi.spyOn(process, "exit").mockImplementation((() => undefined) as never);

    await bootDsh({ boot, env: {}, secretDir: "/tmp/secret", readSecretFile: () => TOKEN });

    expect(boot).toHaveBeenCalledTimes(1);
    const args = boot.mock.calls[0] as unknown[];
    expect(args[0]).toBe("game-agent-v2");
    expect(args[1]).toBe(cordisConfigPath());
    expect(args[2]).toBeUndefined();
    expect(args[3]).toBeUndefined();
    // The bare-module anchor pins plugin resolution at this module.
    expect(String(args[4])).toContain("dsh.ts");
  });
});
