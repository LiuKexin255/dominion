/**
 * saolei-loop plugin entry tests (specs/074-dsh-grpc-runtime/contracts/
 * team-assembly.md §2/§4; specs/078-saolei-js-hooks/contracts/
 * saolei-hooks-plugin.md §5): `apply` mounts each flat `config.plugins` row as
 * a child plugin by bare package name (with the row's config), then reads back
 * the capability provide faces — a missing required face (`saoleiTools`,
 * `memoryMount`) fails the composition load (fail-loud), while the optional
 * hooks face (`saoleiHooks`) is checked only when its row is listed. `apply`
 * does NOT register anything itself: the member-scope registration is the
 * orchestrator's job.
 *
 * Pattern (style/javascript.md Mock convention): the bare-name importer is a
 * `vi.fn()` double returning stub plugin objects (which provide the faces on
 * their own context), so the assembly is exercised without real packages and
 * without module interception.
 */

import { Context } from "@deepseek-ai/cordis";
import type { Plugin } from "@deepseek-ai/cordis";
import { describe, expect, it, vi } from "vitest";

import { apply, Config, inject } from "./index.js";

/** Local stub shape: cordis accepts an object plugin with `apply`. */
interface StubPlugin {
  readonly name: string;
  apply(ctx: Context, config?: unknown): void;
}

/** A stub provider plugin that provides `face` with `value`. */
function providerPlugin(face: string, value: unknown): StubPlugin {
  return {
    name: `stub:${face}`,
    apply(ctx: Context): void {
      ctx.provide(face, value);
    },
  };
}

/** An importer resolving names through the given stub table. */
function stubImporter(
  table: Record<string, StubPlugin>,
): (name: string) => Promise<Plugin> {
  return vi.fn(async (name: string): Promise<Plugin> => {
    const plugin = table[name];
    if (plugin === undefined) {
      throw new Error(`unknown stub plugin ${name}`);
    }
    return plugin as unknown as Plugin;
  });
}

describe("saolei-loop plugin dependency assembly", () => {
  it(
    "mounts every configured row with its config and reads back the faces",
    async () => {
    const seen: Array<{ name: string; config: unknown }> = [];
    const saolei = providerPlugin("saoleiTools", { tools: [], sections: [] });
    const memory = providerPlugin("memoryMount", { build: () => ({}) });
    const importPlugin = vi.fn(async (name: string): Promise<Plugin> => {
      const base = name.includes("memory") ? memory : saolei;
      return {
        name,
        apply(ctx: Context, config: unknown): void {
          seen.push({ name, config });
          base.apply(ctx, undefined);
        },
      } as unknown as Plugin;
    });
    const ctx = new Context();

    await apply(
      ctx,
      {
        plugins: [
          { name: "@stub/saolei" },
          { name: "@stub/memory", config: { target: "t", protoPath: "p" } },
        ],
      },
      { importPlugin },
    );

    expect(importPlugin).toHaveBeenCalledTimes(2);
    expect(importPlugin).toHaveBeenNthCalledWith(1, "@stub/saolei");
    expect(importPlugin).toHaveBeenNthCalledWith(2, "@stub/memory");
    expect(seen).toEqual([
      { name: "@stub/saolei", config: undefined },
      { name: "@stub/memory", config: { target: "t", protoPath: "p" } },
    ]);
    expect(ctx.get("saoleiTools")).toBeDefined();
    expect(ctx.get("memoryMount")).toBeDefined();
  });

  it("fails loud when a required provide face is missing", async () => {
    const importPlugin = stubImporter({
      "@stub/saolei": providerPlugin("saoleiTools", {
        tools: [],
        sections: [],
      }),
      // Provides nothing → memoryMount stays absent.
      "@stub/noop": { name: "stub:noop", apply: () => {} },
    });
    const ctx = new Context();

    await expect(
      apply(
        ctx,
        { plugins: [{ name: "@stub/saolei" }, { name: "@stub/noop" }] },
        { importPlugin },
      ),
    ).rejects.toThrow(/"memoryMount" capability is not mounted/);
  });

  it("fails loud when saoleiTools itself is missing", async () => {
    const importPlugin = stubImporter({
      "@stub/noop": { name: "stub:noop", apply: () => {} },
    });

    await expect(
      apply(
        new Context(),
        { plugins: [{ name: "@stub/noop" }] },
        { importPlugin },
      ),
    ).rejects.toThrow(/"saoleiTools" capability is not mounted/);
  });

  it("fails loud when the hooks row is listed but provides no face", async () => {
    const importPlugin = stubImporter({
      "@stub/saolei": providerPlugin("saoleiTools", { tools: [], sections: [] }),
      "@stub/memory": providerPlugin("memoryMount", { build: () => ({}) }),
      // The listed row mounts but provides nothing → composition error.
      "@dominion/dsh-saolei-hooks": { name: "stub:noop-hooks", apply: () => {} },
    });

    await expect(
      apply(
        new Context(),
        {
          plugins: [
            { name: "@stub/saolei" },
            { name: "@stub/memory" },
            { name: "@dominion/dsh-saolei-hooks" },
          ],
        },
        { importPlugin },
      ),
    ).rejects.toThrow(/"saoleiHooks" capability is not mounted/);
  });

  it("mounts the listed hooks row when it provides the face", async () => {
    const importPlugin = stubImporter({
      "@stub/saolei": providerPlugin("saoleiTools", { tools: [], sections: [] }),
      "@stub/memory": providerPlugin("memoryMount", { build: () => ({}) }),
      "@dominion/dsh-saolei-hooks": providerPlugin("saoleiHooks", {
        buildHooksUnit: () => ({}),
      }),
    });
    const ctx = new Context();

    await apply(
      ctx,
      {
        plugins: [
          { name: "@stub/saolei" },
          { name: "@stub/memory" },
          { name: "@dominion/dsh-saolei-hooks" },
        ],
      },
      { importPlugin },
    );

    expect(ctx.get("saoleiHooks")).toBeDefined();
  });

  it("accepts a composition without the hooks row (optional capability absent)", async () => {
    const importPlugin = stubImporter({
      "@stub/saolei": providerPlugin("saoleiTools", { tools: [], sections: [] }),
      "@stub/memory": providerPlugin("memoryMount", { build: () => ({}) }),
    });
    const ctx = new Context();

    await apply(
      ctx,
      { plugins: [{ name: "@stub/saolei" }, { name: "@stub/memory" }] },
      { importPlugin },
    );

    expect(ctx.get("saoleiHooks")).toBeUndefined();
  });

  it("rejects when a configured plugin fails to mount", async () => {
    const broken: StubPlugin = {
      name: "stub:broken",
      apply: () => {
        throw new Error("broken plugin startup");
      },
    };
    const importPlugin = stubImporter({ "@stub/broken": broken });

    await expect(
      apply(
        new Context(),
        { plugins: [{ name: "@stub/broken" }] },
        { importPlugin },
      ),
    ).rejects.toThrow(/broken plugin startup/);
  });

  it("mounts no row and fails loud on an empty dependency list", async () => {
    await expect(
      apply(new Context(), { plugins: [] }, { importPlugin: stubImporter({}) }),
    ).rejects.toThrow(/"saoleiTools" capability is not mounted/);
  });

  it("defaults the flat dependency list to empty and injects the game foundation", () => {
    expect(Config({ plugins: undefined })).toEqual({ plugins: [] });
    // The game foundation face is an upstream service: the top-level saolei row
    // must be composed or the plugin fails to load (cordis inject resolution;
    // specs/075-saolei-game-foundation/contracts/team-loop-assembly.md §1).
    expect(inject).toEqual(["saolei"]);
  });
});
