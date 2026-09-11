import { describe, expect, it, vi } from "vitest";

import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { dump, load } from "js-yaml";

import { Config, createPresetAuthoring } from "./index.js";
import { nodeDeriveFs } from "./derive.js";
import { MemoryPresetStore } from "./store.js";

import type { Context } from "@deepseek-ai/cordis";
import type { AgentPreset } from "@deepseek-ai/dsh-agent-presets";
import type { PresetAuthoringConfig } from "./index.js";
import type { DeriveFs, RosterSeam } from "./derive.js";
import type { PresetAuthoringDeps } from "./index.js";
import type { PresetRecord } from "./store.js";

const TEMPLATE_COMPOSITION = [
  { id: "persona", name: "@deepseek-ai/dsh-persona", config: { text: "placeholder" } },
  { id: "demo-echo", name: "@dominion/dsh-demo-echo" },
];

/**
 * A test-side roster double over a real template directory: `list` answers
 * the discovered pool templates (and any registered broken row) exactly as
 * the official roster's discovery would. There is no copy/remove surface to
 * double anymore — authoring is store-only and composes derive their
 * composition.
 */
async function rosterHarness() {
  const base = await mkdtemp(join(tmpdir(), "preset-authoring-"));
  const templatesRoot = join(base, "templates");
  await mkdir(join(templatesRoot, "demo-tools"), { recursive: true });
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

  /** id → preset mirroring roster discovery (broken presets resolve too). */
  const entries = new Map<string, AgentPreset>();
  entries.set("demo-tools", {
    id: "demo-tools",
    trust: "system",
    path: join(templatesRoot, "demo-tools", "agent.cordis.yml"),
  });

  const list = vi.fn<() => Promise<AgentPreset[]>>(async () => [...entries.values()]);

  const roster = { list } as unknown as RosterSeam;

  // The real fs seam with mock bookkeeping: derived temp directories are
  // tracked so each harness can clean them up, and write calls are
  // positively asserted (create must produce no file).
  const realFs = nodeDeriveFs();
  const tempDirs: string[] = [];
  const readTextFile = vi.fn(realFs.readTextFile);
  const writeTextFile = vi.fn(realFs.writeTextFile);
  const createTempDir = vi.fn(async (prefix: string) => {
    const dir = await realFs.createTempDir(prefix);
    tempDirs.push(dir);
    return dir;
  });
  const fs: DeriveFs = { readTextFile, createTempDir, writeTextFile };

  const mount = vi.fn(
    async (_agentCtx: Context, _preset: AgentPreset): Promise<void> => undefined,
  );

  const cleanup = async (): Promise<void> => {
    await rm(base, { recursive: true, force: true });
    await Promise.all(tempDirs.map((dir) => rm(dir, { recursive: true, force: true })));
  };
  return {
    base,
    templatesRoot,
    roster,
    list,
    mount,
    fs,
    readTextFile,
    writeTextFile,
    entries,
    cleanup,
  };
}

/** Service under test with the real fs seam and the harness roster. */
async function serviceHarness(overrides: Partial<PresetAuthoringDeps> = {}) {
  const harness = await rosterHarness();
  const store = new MemoryPresetStore();
  const service = createPresetAuthoring({} as never, {
    roster: harness.roster,
    store,
    fs: harness.fs,
    mount: harness.mount,
    ...overrides,
  });
  return { ...harness, store, service };
}

/** Insert a record directly, bypassing create's validation (legacy store state). */
async function storeRecord(
  store: MemoryPresetStore,
  input: Partial<PresetRecord> & { id: string },
): Promise<void> {
  const now = new Date("2026-09-11T00:00:00Z");
  await store.create({ template: "demo-tools", persona: "", createTime: now, updateTime: now, ...input });
}

/** Parse a derived composition file and return its rows. */
async function readRows(path: string): Promise<Array<{ name?: string; config?: { text?: string } }>> {
  return load(await readFile(path, "utf8")) as Array<{
    name?: string;
    config?: { text?: string };
  }>;
}

describe("compose", () => {
  it("derives the stored record over its pool template and mounts the derived composition", async () => {
    const { service, mount, writeTextFile } = await serviceHarness();
    await service.create({
      id: "mine",
      template: "demo-tools",
      role: "planner",
      persona: "P1",
      displayName: "Mine",
    });
    // Store-only create: no composition file is produced.
    expect(writeTextFile).not.toHaveBeenCalled();

    const result = await service.compose("mine");

    expect(result.agentPreset).toBe("mine");
    const agentCtx = {} as never;
    await result.setup(agentCtx);

    expect(mount).toHaveBeenCalledOnce();
    const [ctxArg, presetArg] = mount.mock.calls[0];
    expect(ctxArg).toBe(agentCtx);
    expect(presetArg).toMatchObject({ id: "mine", trust: "user" });
    const rows = await readRows(presetArg.path);
    expect(rows[0]?.name).toBe("@deepseek-ai/dsh-persona");
    expect(rows[0]?.config?.text).toBe("P1");
    expect(rows[1]?.name).toBe("@dominion/dsh-demo-echo");
  });

  it("derives the template persona base when the stored persona is empty", async () => {
    const { service, mount } = await serviceHarness();
    await service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "" });

    const result = await service.compose("mine");
    await result.setup({} as never);

    const rows = await readRows(mount.mock.calls[0][1].path);
    expect(rows[0]?.config?.text).toBe("placeholder");
  });

  it("reads the roster once for the pool template — the stored preset id never reaches it", async () => {
    const { service, list } = await serviceHarness();
    await service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" });
    list.mockClear();

    await service.compose("mine");

    // One live root scan: the template lookup. The stored preset id is a
    // store-only key (the retired roster resolve of the user preset is gone).
    expect(list).toHaveBeenCalledTimes(1);
  });

  it("fails NOT_FOUND before reading the roster when the store has no record", async () => {
    const { service, list, mount } = await serviceHarness();

    const err = await service.compose("missing").then(
      () => undefined,
      (e: unknown) => e,
    );

    expect(err).toMatchObject({
      code: "NOT_FOUND",
      message: expect.stringContaining("missing"),
    });
    expect(list).not.toHaveBeenCalled();
    expect(mount).not.toHaveBeenCalled();
  });

  it("rejects an id-less compose with INVALID_ARGUMENT (preset selection is mandatory)", async () => {
    const { service, store } = await serviceHarness();
    await storeRecord(store, { id: "mine" });

    await expect(service.compose()).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
      message: expect.stringContaining("preset id is required"),
    });
  });

  it("maps an unknown template on a stored record to INVALID_ARGUMENT and never mounts", async () => {
    const { service, store, mount } = await serviceHarness();
    await storeRecord(store, { id: "mine", template: "ghost" });

    await expect(service.compose("mine")).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
      message: expect.stringContaining("ghost"),
    });
    expect(mount).not.toHaveBeenCalled();
  });

  it("fails a broken template with INVALID_ARGUMENT and never mounts", async () => {
    const { service, store, entries, mount } = await serviceHarness();
    entries.set("broken-tools", {
      id: "broken-tools",
      trust: "system",
      path: "/templates/broken-tools/agent.cordis.yml",
      broken: "agent.cordis.yml does not parse",
    });
    await storeRecord(store, { id: "mine", template: "broken-tools" });

    await expect(service.compose("mine")).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
      message: expect.stringContaining("does not parse"),
    });
    expect(mount).not.toHaveBeenCalled();
  });

  it("is idempotent: two composes derive byte-equivalent compositions at distinct paths", async () => {
    const { service, mount } = await serviceHarness();
    await service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" });

    await (await service.compose("mine")).setup({} as never);
    await (await service.compose("mine")).setup({} as never);

    const first = mount.mock.calls[0][1];
    const second = mount.mock.calls[1][1];
    expect(first.path).not.toBe(second.path);
    expect(await readFile(first.path, "utf8")).toBe(await readFile(second.path, "utf8"));
  });

  it("maps an unexpected roster discovery failure to INTERNAL (contract §6)", async () => {
    const harness = await serviceHarness();
    const boom = new Error("disk on fire");
    const list = vi.fn<() => Promise<AgentPreset[]>>().mockRejectedValue(boom);
    const service = createPresetAuthoring({} as never, {
      roster: { list } as unknown as RosterSeam,
      store: harness.store,
      fs: harness.fs,
      mount: harness.mount,
    });
    await storeRecord(harness.store, { id: "mine" });

    const err = await service.compose("mine").then(
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

  it("wraps a derivation failure as INTERNAL and never mounts", async () => {
    const { service, templatesRoot, mount } = await serviceHarness();
    await service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" });
    // The template file disappears after create: the derivation read fails.
    await rm(join(templatesRoot, "demo-tools", "agent.cordis.yml"));

    await expect(service.compose("mine")).rejects.toMatchObject({
      code: "INTERNAL",
      message: expect.stringContaining("deriving preset"),
    });
    expect(mount).not.toHaveBeenCalled();
  });
});

describe("create (store-only authoring)", () => {
  it("records the dynamic fields in the store and only reads the pool template", async () => {
    const { service, store, list, writeTextFile } = await serviceHarness();

    const view = await service.create({
      id: "mine",
      template: "demo-tools",
      role: "planner",
      persona: "P1",
      displayName: "Mine",
    });

    expect(view).toMatchObject({
      id: "mine",
      template: "demo-tools",
      role: "planner",
      persona: "P1",
      displayName: "Mine",
    });
    // The store holds the dynamic fields, not composition content (D2).
    expect(await store.list()).toHaveLength(1);
    expect((await store.get("mine")).persona).toBe("P1");
    // Two live roster scans: the template lookup and the id-taken check.
    expect(list).toHaveBeenCalledTimes(2);
    // Store-only: no composition file is produced by create.
    expect(writeTextFile).not.toHaveBeenCalled();
  });

  it("rejects a duplicate id with ALREADY_EXISTS", async () => {
    const { service } = await serviceHarness();
    await service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" });

    await expect(
      service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P2" }),
    ).rejects.toMatchObject({ code: "ALREADY_EXISTS" });
  });

  it("rejects an unknown template with INVALID_ARGUMENT and leaves no residue", async () => {
    const { service, store, writeTextFile } = await serviceHarness();

    await expect(
      service.create({ id: "mine", template: "ghost", role: "planner", persona: "P1" }),
    ).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
    expect(await store.list()).toEqual([]);
    expect(writeTextFile).not.toHaveBeenCalled();
  });

  it("rejects an id outside the preset grammar with INVALID_ARGUMENT before any roster call", async () => {
    const { service, store, list } = await serviceHarness();

    await expect(
      service.create({ id: "Bad Id", template: "demo-tools", role: "planner", persona: "" }),
    ).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
    expect(list).not.toHaveBeenCalled();
    expect(await store.list()).toEqual([]);
  });

  it("refuses an id a roster root already supplies (shipped templates cannot be shadowed)", async () => {
    const { service, store } = await serviceHarness();

    await expect(
      service.create({ id: "demo-tools", template: "demo-tools", role: "player", persona: "" }),
    ).rejects.toMatchObject({ code: "ALREADY_EXISTS" });
    expect(await store.list()).toEqual([]);
  });

  it("enforces the caller-declared template row rules before the store write (T021)", async () => {
    const { service, store, templatesRoot } = await serviceHarness({
      templateRules: {
        "demo-tools": {
          required: ["@dominion/dsh-demo-echo"],
          forbidden: ["@dominion/dsh-memory/preset-row"],
        },
      },
    });

    // The template carries exactly the required row and none of the
    // forbidden ones: the record lands normally.
    const view = await service.create({ id: "mine", template: "demo-tools", role: "player", persona: "P1" });
    expect(view.id).toBe("mine");
    expect(await store.list()).toHaveLength(1);

    // A template missing the required row is rejected BEFORE the store write
    // (fail-fast: no record, no residue).
    await writeFile(
      join(templatesRoot, "demo-tools", "agent.cordis.yml"),
      dump([{ id: "persona", name: "@deepseek-ai/dsh-persona", config: { text: "placeholder" } }], {
        lineWidth: -1,
      }),
      "utf8",
    );
    await expect(
      service.create({ id: "broken", template: "demo-tools", role: "player", persona: "P1" }),
    ).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
      message: expect.stringContaining("@dominion/dsh-demo-echo"),
    });
    expect(await store.list()).toHaveLength(1);

    // A forbidden row is rejected the same way.
    await writeFile(
      join(templatesRoot, "demo-tools", "agent.cordis.yml"),
      dump(
        [
          { id: "persona", name: "@deepseek-ai/dsh-persona", config: { text: "placeholder" } },
          { id: "echo", name: "@dominion/dsh-demo-echo" },
          { id: "memory", name: "@dominion/dsh-memory/preset-row" },
        ],
        { lineWidth: -1 },
      ),
      "utf8",
    );
    await expect(
      service.create({ id: "broken2", template: "demo-tools", role: "planner", persona: "P1" }),
    ).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
      message: expect.stringContaining("must not contain"),
    });

    // A duplicated required row violates the exactly-one semantics.
    await writeFile(
      join(templatesRoot, "demo-tools", "agent.cordis.yml"),
      dump(
        [
          { id: "persona", name: "@deepseek-ai/dsh-persona", config: { text: "placeholder" } },
          { id: "echo-a", name: "@dominion/dsh-demo-echo" },
          { id: "echo-b", name: "@dominion/dsh-demo-echo" },
        ],
        { lineWidth: -1 },
      ),
      "utf8",
    );
    await expect(
      service.create({ id: "broken3", template: "demo-tools", role: "player", persona: "P1" }),
    ).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
      message: expect.stringContaining("exactly one"),
    });
    expect(await store.list()).toHaveLength(1);
  });

  it("leaves templates without a configured rule table unvalidated (generic consumers)", async () => {
    const { service, store } = await serviceHarness();

    // No templateRules configured: the existing composition (persona +
    // demo-echo) is accepted even though no rule table names it.
    const view = await service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" });

    expect(view.id).toBe("mine");
    expect(await store.list()).toHaveLength(1);
  });

  it("surfaces a store write failure as the service error (no copy to roll back)", async () => {
    const failingStore = new MemoryPresetStore();
    vi.spyOn(failingStore, "create").mockRejectedValue(new Error("store down"));
    const { service, writeTextFile } = await serviceHarness({ store: failingStore });

    await expect(
      service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" }),
    ).rejects.toMatchObject({ message: expect.stringContaining("store down") });
    expect(writeTextFile).not.toHaveBeenCalled();
  });
});

describe("get/list/update/remove", () => {
  it("maps an unknown id to NOT_FOUND on get", async () => {
    const { service } = await serviceHarness();

    await expect(service.get("missing")).rejects.toMatchObject({ code: "NOT_FOUND" });
  });

  it("updates the persona in the store record and refreshes updateTime", async () => {
    const { service, store, list, writeTextFile } = await serviceHarness();
    await service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" });
    const before = await store.get("mine");
    list.mockClear();
    writeTextFile.mockClear();

    const view = await service.update("mine", { persona: "P2" });

    expect(view.persona).toBe("P2");
    const updated = await store.get("mine");
    expect(updated.persona).toBe("P2");
    expect(updated.updateTime.getTime()).toBeGreaterThanOrEqual(before.updateTime.getTime());
    // Store-only: no roster access and no file write on update.
    expect(list).not.toHaveBeenCalled();
    expect(writeTextFile).not.toHaveBeenCalled();
  });

  it("stores an empty persona as-is (the derivation resolves the template base)", async () => {
    const { service, store } = await serviceHarness();
    await service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" });

    const view = await service.update("mine", { persona: "" });

    expect(view.persona).toBe("");
    expect((await store.get("mine")).persona).toBe("");
  });

  it("updates displayName in the store record only", async () => {
    const { service, store, writeTextFile } = await serviceHarness();
    await service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" });

    const view = await service.update("mine", { displayName: "Renamed" });

    expect(view.displayName).toBe("Renamed");
    expect((await store.get("mine")).displayName).toBe("Renamed");
    expect(writeTextFile).not.toHaveBeenCalled();
  });

  it("maps an unknown update target to NOT_FOUND", async () => {
    const { service } = await serviceHarness();

    await expect(service.update("missing", { persona: "P2" })).rejects.toMatchObject({
      code: "NOT_FOUND",
    });
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

  it("removes only the store record; no composition file or roster deletion exists", async () => {
    const { service, store, list } = await serviceHarness();
    await service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" });
    list.mockClear();

    await service.remove("mine");

    expect(await store.list()).toEqual([]);
    // One live roster scan: the shipped-id protection probe.
    expect(list).toHaveBeenCalledTimes(1);
    await expect(service.get("mine")).rejects.toMatchObject({ code: "NOT_FOUND" });
  });

  it("maps an unknown remove target to NOT_FOUND and a template to FAILED_PRECONDITION", async () => {
    const { service, store } = await serviceHarness();

    await expect(service.remove("missing")).rejects.toMatchObject({ code: "NOT_FOUND" });
    await expect(service.remove("demo-tools")).rejects.toMatchObject({
      code: "FAILED_PRECONDITION",
    });
    expect(await store.list()).toEqual([]);
  });
});

describe("Config", () => {
  it("validates the caller-declared template row rules and defaults them empty", () => {
    const config = Config({
      storage: "mongo",
      mongoUri: "mongodb://x",
      mongoDatabase: "db",
      mongoCollection: "c",
      templateRules: {
        player: {
          required: ["@dominion/dsh-saolei"],
          forbidden: ["@dominion/dsh-memory", "@dominion/dsh-memory/preset-row"],
        },
      },
    });

    expect(config.templateRules).toEqual({
      player: {
        required: ["@dominion/dsh-saolei"],
        forbidden: ["@dominion/dsh-memory", "@dominion/dsh-memory/preset-row"],
      },
    });
    // Omitted rules default to an empty table: generic deployments validate
    // nothing (the host opts in per template).
    expect(Config({ storage: "memory" }).templateRules).toEqual({});
  });

  it("defaults a one-sided rule table's omitted half to an empty list", () => {
    // A host YAML value reaches the schema untyped; the declared Config input
    // mirrors the validated output shape, hence the widening cast.
    const raw: unknown = {
      storage: "memory",
      templateRules: {
        player: { required: ["@dominion/dsh-saolei"] },
        planner: { forbidden: ["@dominion/dsh-memory/preset-row"] },
      },
    };
    const config = Config(raw as PresetAuthoringConfig);

    expect(config.templateRules).toEqual({
      player: { required: ["@dominion/dsh-saolei"], forbidden: [] },
      planner: { required: [], forbidden: ["@dominion/dsh-memory/preset-row"] },
    });
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
