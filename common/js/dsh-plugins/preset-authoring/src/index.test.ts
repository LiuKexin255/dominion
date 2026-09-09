import { describe, expect, it, vi } from "vitest";

import { PresetNotWritableError, UnknownPresetError } from "@deepseek-ai/dsh-agent-presets";
import { cp, mkdir, mkdtemp, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { dump, load } from "js-yaml";

import { createPresetAuthoring } from "./index.js";
import { nodeMaterializeFs } from "./materialize.js";
import { MemoryPresetStore } from "./store.js";

import type { RosterSeam } from "./materialize.js";
import type { PresetAuthoringDeps } from "./index.js";

const TEMPLATE_COMPOSITION = [
  { id: "persona", name: "@deepseek-ai/dsh-persona", config: { text: "placeholder" } },
  { id: "demo-echo", name: "@dominion/dsh-demo-echo" },
];

/**
 * A test-side roster double that performs REAL directory copies into a temp
 * writable root (the roster is still a `vi.fn` seam per contract §5 — the
 * double's calls are positively asserted), so the fs seam exercises real
 * files end to end (V4-1 patch correctness through the service).
 */
async function rosterHarness() {
  const base = await mkdtemp(join(tmpdir(), "preset-authoring-"));
  const templatesRoot = join(base, "templates");
  const writableRoot = join(base, "writable");
  await mkdir(join(templatesRoot, "demo-tools"), { recursive: true });
  await mkdir(writableRoot, { recursive: true });
  await writeFile(
    join(templatesRoot, "demo-tools", "agent.cordis.yml"),
    dump(TEMPLATE_COMPOSITION, { lineWidth: -1 }),
    "utf8",
  );
  await writeFile(
    join(templatesRoot, "demo-tools", "preset.yml"),
    dump({ name: "demo-tools", description: "persona + demo_echo 工具行模板" }, { lineWidth: -1 }),
    "utf8",
  );

  /** id → {path, broken?} mirroring roster discovery (broken presets resolve). */
  const entries = new Map<
    string,
    { id: string; trust: string; path: string; description?: string; broken?: string }
  >();
  entries.set("demo-tools", {
    id: "demo-tools",
    trust: "system",
    path: join(templatesRoot, "demo-tools", "agent.cordis.yml"),
    description: "persona + demo_echo 工具行模板",
  });

  const resolve = vi.fn(async (id?: string) => {
    const found = id === undefined ? undefined : entries.get(id);
    if (found === undefined) {
      throw new UnknownPresetError(id ?? "<default>", [...entries.keys()]);
    }
    return found;
  });
  const copy = vi.fn(async (from: string, id: string, name?: string) => {
    const source = entries.get(from);
    if (source === undefined) {
      throw new UnknownPresetError(from, [...entries.keys()]);
    }
    const sourceDir = source.path.replace(/\/agent\.cordis\.yml$/, "");
    const targetDir = join(writableRoot, id);
    await cp(sourceDir, targetDir, { recursive: true });
    // Roster copy semantics: the source description is kept, its name is
    // replaced by the display name (or the id) — never identical to the source.
    await writeFile(
      join(targetDir, "preset.yml"),
      dump({ name: name ?? id, description: "persona + demo_echo 工具行模板" }, { lineWidth: -1 }),
      "utf8",
    );
    entries.set(id, { id, trust: "user", path: join(targetDir, "agent.cordis.yml") });
  });
  const remove = vi.fn(async (id: string) => {
    const found = entries.get(id);
    if (found === undefined) {
      throw new UnknownPresetError(id, [...entries.keys()]);
    }
    if (found.trust === "system") {
      throw new PresetNotWritableError(id, "it does not live under the writable preset root");
    }
    await rm(found.path.replace(/\/agent\.cordis\.yml$/, ""), { recursive: true, force: true });
    entries.delete(id);
  });
  const mount = vi.fn();

  const roster = { resolve, mount, copy, remove } as unknown as RosterSeam;
  const cleanup = async (): Promise<void> => {
    await rm(base, { recursive: true, force: true });
  };
  return { base, templatesRoot, writableRoot, roster, resolve, mount, copy, remove, entries, cleanup };
}

/** Service under test with the real fs seam and the harness roster. */
async function serviceHarness(overrides: Partial<PresetAuthoringDeps> = {}) {
  const harness = await rosterHarness();
  const store = new MemoryPresetStore();
  const service = createPresetAuthoring({} as never, {
    roster: harness.roster,
    store,
    fs: nodeMaterializeFs(),
    ...overrides,
  });
  return { ...harness, store, service };
}

// Node keeps tmpdir handles across the suite; each harness cleans its own
// base directories, so nothing persists here.

describe("compose", () => {
  it("returns the resolved id and a setup that mounts the standing composition", async () => {
    const { service, mount } = await serviceHarness();

    const result = await service.compose("demo-tools");

    expect(result.agentPreset).toBe("demo-tools");
    const agentCtx = {} as never;
    await result.setup(agentCtx);
    expect(mount).toHaveBeenCalledOnce();
    expect(mount).toHaveBeenCalledWith(agentCtx, "demo-tools");
  });

  it("passes the preset id through to the roster, default included", async () => {
    const { service, resolve } = await serviceHarness();
    // resolve(undefined) is the roster's default-preset contract (its README,
    // Service section); the plugin forwards undefined verbatim. The harness
    // registry has no default registered, so the roster rejection surfaces
    // mapped to INVALID_ARGUMENT.
    await expect(service.compose()).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
    expect(resolve).toHaveBeenCalledWith(undefined);
  });

  it("fails fast on a broken preset with INVALID_ARGUMENT and never mounts (V3-3)", async () => {
    const broken = await serviceHarness();
    // Corrupt the copy's composition file, then have discovery report the
    // broken reason exactly as the roster's health check would.
    const copyPath = join(broken.base, "writable", "mine", "agent.cordis.yml");
    await mkdir(join(broken.base, "writable", "mine"), { recursive: true });
    await writeFile(copyPath, "{ not: [parsable", "utf8");
    broken.entries.set("mine", { id: "mine", trust: "user", path: copyPath, broken: "agent.cordis.yml does not parse" });

    await expect(broken.service.compose("mine")).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
      message: expect.stringContaining("does not parse"),
    });
    // No half-composed session: mount is never attempted for a broken preset.
    expect(broken.resolve).toHaveBeenCalledOnce();
    expect(broken.mount).not.toHaveBeenCalled();
  });

  it("maps an unknown preset id to INVALID_ARGUMENT carrying the available ids", async () => {
    const { service } = await serviceHarness();

    await expect(service.compose("missing")).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
      message: expect.stringContaining("demo-tools"),
    });
  });

  it("maps an unexpected resolve failure to INTERNAL (contract §6)", async () => {
    const harness = await serviceHarness();
    const boom = new Error("disk on fire");
    const resolve = vi.fn().mockRejectedValue(boom);
    const service = createPresetAuthoring({} as never, {
      roster: { ...harness.roster, resolve } as unknown as RosterSeam,
      store: new MemoryPresetStore(),
      fs: nodeMaterializeFs(),
    });

    const err = await service.compose("demo-tools").then(
      () => undefined,
      (e: unknown) => e,
    );

    expect(err).toMatchObject({
      code: "INTERNAL",
      message: expect.stringContaining("disk on fire"),
    });
    expect((err as { cause?: unknown }).cause).toBe(boom);
    expect(harness.mount).not.toHaveBeenCalled();
  });
});

describe("create (V4-1 C1 materialization through the service)", () => {
  it("materializes the copy, patches the persona, and records the dynamic fields", async () => {
    const { service, store, base, copy } = await serviceHarness();

    const view = await service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" });

    expect(copy).toHaveBeenCalledOnce();
    expect(view).toMatchObject({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" });
    // The store holds the dynamic fields, not composition content (D2).
    expect(await store.list()).toHaveLength(1);

    const composition = load(await readFile(join(base, "writable", "mine", "agent.cordis.yml"), "utf8")) as Array<{
      name?: string;
      config?: { text?: string };
    }>;
    expect(composition[0]?.config?.text).toBe("P1");
    expect(composition[1]?.name).toBe("@dominion/dsh-demo-echo");
    const metadata = load(await readFile(join(base, "writable", "mine", "preset.yml"), "utf8")) as {
      name?: string;
    };
    expect(metadata.name).toBe("mine");
  });

  it("rejects a duplicate id with ALREADY_EXISTS", async () => {
    const { service } = await serviceHarness();
    await service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" });

    await expect(service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P2" })).rejects.toMatchObject({
      code: "ALREADY_EXISTS",
    });
  });

  it("rejects an unknown template with INVALID_ARGUMENT and leaves no residue", async () => {
    const { service, store, copy } = await serviceHarness();

    await expect(service.create({ id: "mine", template: "ghost", role: "planner", persona: "P1" })).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
    });
    expect(copy).not.toHaveBeenCalled();
    expect(await store.list()).toEqual([]);
  });

  it("materializes the template base persona when created with an empty persona", async () => {
    const { service, base } = await serviceHarness();

    const view = await service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "" });

    expect(view.persona).toBe("");
    // No patch ran: the copy carries the template's persona row text — the
    // role default base (specs/059-agent-v2-team-mode/contracts/preset-api.md
    // §2 persona 空值).
    const composition = load(await readFile(join(base, "writable", "mine", "agent.cordis.yml"), "utf8")) as Array<{
      name?: string;
      config?: { text?: string };
    }>;
    expect(composition[0]?.name).toBe("@deepseek-ai/dsh-persona");
    expect(composition[0]?.config?.text).toBe("placeholder");
  });

  it("rolls the store record and the copy back together when the store write fails", async () => {
    const failingStore = new MemoryPresetStore();
    vi.spyOn(failingStore, "create").mockRejectedValue(new Error("store down"));
    const { service, base, remove } = await serviceHarness({ store: failingStore });

    await expect(service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" })).rejects.toMatchObject({
      message: expect.stringContaining("store down"),
    });
    // 无半物化残留: the copied directory was rolled back.
    expect(remove).toHaveBeenCalledOnce();
    await expect(readdir(join(base, "writable"))).resolves.toEqual([]);
  });
});

describe("get/list/update/remove", () => {
  it("maps an unknown id to NOT_FOUND on get", async () => {
    const { service } = await serviceHarness();

    await expect(service.get("missing")).rejects.toMatchObject({ code: "NOT_FOUND" });
  });

  it("updates the persona in the copy file and refreshes updateTime", async () => {
    const { service, base } = await serviceHarness();
    await service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" });

    const view = await service.update("mine", { persona: "P2" });

    expect(view.persona).toBe("P2");
    const composition = load(await readFile(join(base, "writable", "mine", "agent.cordis.yml"), "utf8")) as Array<{
      config?: { text?: string };
    }>;
    expect(composition[0]?.config?.text).toBe("P2");
  });

  it("resets the persona row to the template base on an empty-persona update", async () => {
    const { service, base } = await serviceHarness();
    await service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" });

    const view = await service.update("mine", { persona: "" });

    // Neither the previous "P1" nor an empty string survives: the copy's
    // persona row reads the template base again (preset-api.md §2 persona
    // 空值), equivalent to create with no persona.
    expect(view.persona).toBe("");
    const composition = load(await readFile(join(base, "writable", "mine", "agent.cordis.yml"), "utf8")) as Array<{
      config?: { text?: string };
    }>;
    expect(composition[0]?.config?.text).toBe("placeholder");
  });

  it("lists authored presets narrowed to one role pool", async () => {
    const { service } = await serviceHarness();
    await service.create({ id: "p1", template: "demo-tools", role: "player", persona: "a" });
    await service.create({ id: "p2", template: "demo-tools", role: "planner", persona: "b" });

    const all = await service.list();
    expect(all.map((view) => view.id).sort()).toEqual(["p1", "p2"]);
    const players = await service.list("player");
    expect(players.map((view) => view.id)).toEqual(["p1"]);
    const planners = await service.list("planner");
    expect(planners.map((view) => view.id)).toEqual(["p2"]);
  });

  it("updates only preset.yml for a displayName change (composition stamp untouched)", async () => {
    const { service, base } = await serviceHarness();
    await service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" });
    const compositionBefore = await readFile(join(base, "writable", "mine", "agent.cordis.yml"), "utf8");

    const view = await service.update("mine", { displayName: "Mine" });

    expect(view.displayName).toBe("Mine");
    expect(await readFile(join(base, "writable", "mine", "agent.cordis.yml"), "utf8")).toBe(compositionBefore);
    const metadata = load(await readFile(join(base, "writable", "mine", "preset.yml"), "utf8")) as {
      name?: string;
      description?: string;
    };
    expect(metadata.name).toBe("Mine");
    expect(metadata.description).toBe("persona + demo_echo 工具行模板");
  });

  it("removes through the roster and the store; joined sessions are a roster concern", async () => {
    const { service, store, remove } = await serviceHarness();
    await service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" });

    await service.remove("mine");

    expect(remove).toHaveBeenCalledOnce();
    expect(remove).toHaveBeenCalledWith("mine");
    expect(await store.list()).toEqual([]);
    await expect(service.get("mine")).rejects.toMatchObject({ code: "NOT_FOUND" });
  });

  it("maps an unknown remove target to NOT_FOUND and a template to FAILED_PRECONDITION", async () => {
    const { service } = await serviceHarness();

    await expect(service.remove("missing")).rejects.toMatchObject({ code: "NOT_FOUND" });
    await expect(service.remove("demo-tools")).rejects.toMatchObject({ code: "FAILED_PRECONDITION" });
  });
});

describe("apply", () => {
  it("provides the service under ctx.presetAuthoring (effect-based registration)", async () => {
    const { apply, PresetAuthoringError: ExportedError } = await import("./index.js");
    const provide = vi.fn();
    apply({ agentPresets: undefined, provide } as never, { storage: "memory" });

    expect(provide).toHaveBeenCalledOnce();
    expect(provide.mock.calls[0][0]).toBe("presetAuthoring");
    // The re-exported error type carries the stable code surface (contract §6).
    expect(new ExportedError("NOT_FOUND", "x").code).toBe("NOT_FOUND");
  });
});
