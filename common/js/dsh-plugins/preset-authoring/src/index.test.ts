import { describe, expect, it, vi } from "vitest";

import { Config, createPresetAuthoring } from "./index.js";
import { PERSONA_ROW_NAME } from "./derive.js";
import { MemoryPresetStore } from "./store.js";

import type { Context } from "@deepseek-ai/cordis";
import type { AgentPreset, PresetDefinition } from "@deepseek-ai/dsh-agent-preset-registry";
import type { PresetAuthoringConfig, PresetAuthoringDeps } from "./index.js";
import type { CompositionRow, TemplateDeclaration } from "./derive.js";
import type { PresetRecord } from "./store.js";

function defineTemplate(prefix: string, description: string): TemplateDeclaration {
  return {
    name: "Demo",
    description,
    plugins: [
      { id: "persona", name: PERSONA_ROW_NAME, config: { prefix } },
      { id: "demo-echo", name: "@dominion/dsh-demo-echo" },
    ],
  };
}

const TEMPLATES: Record<string, TemplateDeclaration> = {
  "demo-tools": defineTemplate("placeholder", "persona + demo_echo 工具行模板"),
};

/** One in-flight registration the fake registry tracks. */
interface FakeRevision {
  readonly definition: PresetDefinition;
  retired: boolean;
}

/**
 * A DI test double for the registry's consumed subset: it records the
 * register/unregister call sequence, exposes activation diagnostics, and keeps
 * a retired revision reachable to a bound agent — the revision-retention
 * contract the update path relies on. No module interception
 * (style/javascript.md Mock convention).
 */
function createFakeRegistry() {
  const active = new Map<string, FakeRevision>();
  const broken = new Map<string, string>();
  const bindings = new Map<Context, FakeRevision>();
  const events: Array<{ action: "register" | "unregister"; id: string }> = [];

  const register = vi.fn(async (definition: PresetDefinition): Promise<() => Promise<void>> => {
    if (active.has(definition.id)) {
      throw new Error(`Duplicate agent preset: ${definition.id}`);
    }
    const revision: FakeRevision = { definition, retired: false };
    active.set(definition.id, revision);
    events.push({ action: "register", id: definition.id });
    return async () => {
      active.delete(definition.id);
      revision.retired = true;
      events.push({ action: "unregister", id: definition.id });
    };
  });

  const resolve = vi.fn(async (id: string): Promise<AgentPreset> => {
    const revision = active.get(id);
    if (revision === undefined) {
      throw new Error(`Unknown agent preset: ${id}`);
    }
    const reason = broken.get(id);
    return { id, ...(reason === undefined ? {} : { broken: reason }) };
  });

  const mount = vi.fn(async (agentCtx: Context, id: string): Promise<AgentPreset> => {
    const revision = active.get(id);
    if (revision === undefined) {
      throw new Error(`Unknown agent preset: ${id}`);
    }
    const reason = broken.get(id);
    if (reason !== undefined) {
      throw new Error(`agent-preset/invalid: ${reason}`);
    }
    bindings.set(agentCtx, revision);
    return { id };
  });

  return { register, resolve, mount, active, broken, bindings, events };
}

/** Engine under test with the fake registry; tests reach `engine.service`. */
function harness(overrides: Partial<PresetAuthoringDeps> = {}) {
  const registry = createFakeRegistry();
  const store = new MemoryPresetStore();
  const engine = createPresetAuthoring({} as never, {
    registry,
    store,
    templates: TEMPLATES,
    ...overrides,
  });
  return { registry, store, engine, service: engine.service };
}

/** Insert a record directly, bypassing create's validation (legacy store state). */
async function storeRecord(
  store: MemoryPresetStore,
  input: Partial<PresetRecord> & { id: string },
): Promise<void> {
  const now = new Date("2026-09-11T00:00:00Z");
  await store.create({ template: "demo-tools", persona: "", createTime: now, updateTime: now, ...input });
}

function prefixOf(row: CompositionRow | undefined): unknown {
  return (row?.config as { prefix?: unknown } | undefined)?.prefix;
}

function revisionPrefix(revision: FakeRevision | undefined): unknown {
  return prefixOf(revision?.definition.plugins[0]);
}

describe("compose", () => {
  it("binds the new agent to the record's registry revision on setup", async () => {
    const { service, registry } = harness();
    await service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" });

    const result = await service.compose("mine");

    expect(result.agentPreset).toBe("mine");
    const agentCtx = {} as unknown as Context;
    await result.setup(agentCtx);

    expect(registry.mount).toHaveBeenCalledOnce();
    expect(registry.mount.mock.calls[0][0]).toBe(agentCtx);
    expect(registry.mount.mock.calls[0][1]).toBe("mine");
    expect(registry.bindings.get(agentCtx)?.definition.id).toBe("mine");
  });

  it("fails NOT_FOUND before touching the registry when the store has no record", async () => {
    const { service, registry } = harness();

    await expect(service.compose("missing")).rejects.toMatchObject({
      code: "NOT_FOUND",
      message: expect.stringContaining("missing"),
    });
    expect(registry.register).not.toHaveBeenCalled();
    expect(registry.mount).not.toHaveBeenCalled();
  });

  it("rejects an id-less compose with INVALID_ARGUMENT (preset selection is mandatory)", async () => {
    const { service } = harness();

    await expect(service.compose()).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
      message: expect.stringContaining("preset id is required"),
    });
  });

  it("maps an unknown template on a stored record to INVALID_ARGUMENT and never binds", async () => {
    const { service, store, registry } = harness();
    await storeRecord(store, { id: "mine", template: "ghost" });

    await expect(service.compose("mine")).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
      message: expect.stringContaining("ghost"),
    });
    expect(registry.mount).not.toHaveBeenCalled();
  });

  it("passes a broken definition's binding failure through registry semantics", async () => {
    const { service, registry } = harness();
    await service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" });
    // Corrupt the revision after create: only the binding path observes it.
    registry.broken.set("mine", "child row is corrupted");

    const result = await service.compose("mine");
    await expect(result.setup({} as never)).rejects.toThrow(/agent-preset\/invalid/);
  });
});

describe("create", () => {
  it("registers the derived definition and records the dynamic fields in the store", async () => {
    const { service, store, registry } = harness();

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
    expect(await store.list()).toHaveLength(1);
    expect(registry.register).toHaveBeenCalledOnce();
    const definition = registry.register.mock.calls[0][0];
    expect(definition.id).toBe("mine");
    expect(definition.name).toBe("Mine");
    expect(definition.description).toBe("persona + demo_echo 工具行模板");
    expect(prefixOf(definition.plugins[0])).toBe("P1");
    expect(definition.plugins[1]?.name).toBe("@dominion/dsh-demo-echo");
    expect(registry.events).toEqual([{ action: "register", id: "mine" }]);
    // The injected template snapshot is never mutated by a derivation.
    expect(prefixOf(TEMPLATES["demo-tools"].plugins[0])).toBe("placeholder");
  });

  it("rejects a duplicate id with ALREADY_EXISTS without re-registering", async () => {
    const { service, registry } = harness();
    await service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" });

    await expect(
      service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P2" }),
    ).rejects.toMatchObject({ code: "ALREADY_EXISTS" });
    expect(registry.register).toHaveBeenCalledOnce();
  });

  it("rejects an unknown template with INVALID_ARGUMENT and leaves no residue", async () => {
    const { service, store, registry } = harness();

    await expect(
      service.create({ id: "mine", template: "ghost", role: "planner", persona: "P1" }),
    ).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
    expect(await store.list()).toEqual([]);
    expect(registry.register).not.toHaveBeenCalled();
  });

  it("rejects an id outside the preset grammar with INVALID_ARGUMENT before registration", async () => {
    const { service, store, registry } = harness();

    await expect(
      service.create({ id: "Bad Id", template: "demo-tools", role: "planner", persona: "" }),
    ).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
    expect(await store.list()).toEqual([]);
    expect(registry.register).not.toHaveBeenCalled();
  });

  it("rejects the empty preset id with INVALID_ARGUMENT and leaves no residue", async () => {
    const { service, store, registry } = harness();

    // The id grammar rejects "", so no roster entry can ever claim the
    // registry's empty selection default — the plugin side of the fail-loud
    // chain locked by agent_v2's dsh.test.ts registry-default assertion
    // (specs/070-dsh-upgrade/contracts/composition-manifest.md §2;
    // specs/070-dsh-upgrade/research.md D4 终态结论).
    await expect(
      service.create({ id: "", template: "demo-tools", role: "planner", persona: "" }),
    ).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
    expect(await store.list()).toEqual([]);
    expect(registry.register).not.toHaveBeenCalled();
  });

  it("refuses an id a snapshot template already owns (shared deployment id space)", async () => {
    const { service, store, registry } = harness();

    await expect(
      service.create({ id: "demo-tools", template: "demo-tools", role: "player", persona: "" }),
    ).rejects.toMatchObject({ code: "ALREADY_EXISTS" });
    expect(await store.list()).toEqual([]);
    expect(registry.register).not.toHaveBeenCalled();
  });

  it("enforces the caller-declared template row rules before the store write", async () => {
    const rules = {
      "demo-tools": { required: ["@dominion/dsh-demo-echo"], forbidden: ["@dominion/dsh-memory"] },
    };

    // A declaration missing the required row is rejected before persistence.
    const missingRequired = harness({
      templates: {
        "demo-tools": { plugins: [{ name: PERSONA_ROW_NAME, config: { prefix: "x" } }] },
      },
      templateRules: rules,
    });
    await expect(
      missingRequired.service.create({ id: "broken", template: "demo-tools", role: "player", persona: "P1" }),
    ).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
      message: expect.stringContaining("@dominion/dsh-demo-echo"),
    });
    expect(await missingRequired.store.list()).toEqual([]);
    expect(missingRequired.registry.register).not.toHaveBeenCalled();

    // A forbidden row is rejected the same way.
    const forbidden = harness({
      templates: {
        "demo-tools": {
          plugins: [
            { name: PERSONA_ROW_NAME, config: { prefix: "x" } },
            { name: "@dominion/dsh-demo-echo" },
            { name: "@dominion/dsh-memory" },
          ],
        },
      },
      templateRules: rules,
    });
    await expect(
      forbidden.service.create({ id: "broken2", template: "demo-tools", role: "planner", persona: "P1" }),
    ).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
      message: expect.stringContaining("must not contain"),
    });

    // A duplicated required row violates the exactly-one semantics.
    const duplicated = harness({
      templates: {
        "demo-tools": {
          plugins: [
            { name: PERSONA_ROW_NAME, config: { prefix: "x" } },
            { name: "@dominion/dsh-demo-echo" },
            { name: "@dominion/dsh-demo-echo" },
          ],
        },
      },
      templateRules: rules,
    });
    await expect(
      duplicated.service.create({ id: "broken3", template: "demo-tools", role: "player", persona: "P1" }),
    ).rejects.toMatchObject({ code: "INVALID_ARGUMENT", message: expect.stringContaining("exactly one") });

    // The rule-compliant declaration lands normally.
    const accepted = harness({ templateRules: rules });
    const view = await accepted.service.create({
      id: "mine",
      template: "demo-tools",
      role: "player",
      persona: "P1",
    });
    expect(view.id).toBe("mine");
    expect(await accepted.store.list()).toHaveLength(1);
  });

  it("leaves templates without a configured rule table unvalidated (generic consumers)", async () => {
    const { service, store } = harness();

    const view = await service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" });

    expect(view.id).toBe("mine");
    expect(await store.list()).toHaveLength(1);
  });

  it("surfaces a store write failure without registering", async () => {
    const failingStore = new MemoryPresetStore();
    vi.spyOn(failingStore, "create").mockRejectedValue(new Error("store down"));
    const { service, registry } = harness({ store: failingStore });

    await expect(
      service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" }),
    ).rejects.toMatchObject({ message: expect.stringContaining("store down") });
    expect(registry.register).not.toHaveBeenCalled();
  });

  it("rolls the store back when registration itself rejects", async () => {
    const { service, store, registry } = harness();
    registry.register.mockRejectedValueOnce(new Error("Duplicate agent preset: mine"));

    await expect(
      service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" }),
    ).rejects.toMatchObject({ code: "INTERNAL" });
    expect(await store.list()).toEqual([]);
  });

  it("releases the registration when resolve fails after a successful register", async () => {
    const { service, store, registry } = harness();
    registry.resolve.mockRejectedValueOnce(new Error("resolve down"));

    await expect(
      service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" }),
    ).rejects.toMatchObject({ code: "INTERNAL" });

    // Both projections stay consistent: the store rolled back and the registry
    // released the registration resolve could not confirm (no orphan definition
    // retained until plugin dispose).
    expect(await store.list()).toEqual([]);
    expect(registry.active.has("mine")).toBe(false);
    expect(registry.events).toEqual([
      { action: "register", id: "mine" },
      { action: "unregister", id: "mine" },
    ]);
  });

  it("leaves an activation-failed definition in the roster, keeps its record, and rejects new bindings", async () => {
    const { service, store, registry } = harness();
    registry.broken.set("bad", "child row is corrupted");

    const err = await service
      .create({ id: "bad", template: "demo-tools", role: "player", persona: "P1" })
      .then(
        () => undefined,
        (e: unknown) => e,
      );

    expect(err).toMatchObject({
      code: "INVALID_ARGUMENT",
      message: expect.stringContaining("activation failed"),
    });
    // The definition stays visible in the roster...
    expect(registry.active.has("bad")).toBe(true);
    // ...and the store, the single source of truth, keeps the record it projected.
    expect((await store.get("bad")).id).toBe("bad");
    // A new agent cannot bind to the broken definition.
    const result = await service.compose("bad");
    await expect(result.setup({} as never)).rejects.toThrow(/agent-preset\/invalid/);
  });
});

describe("get/list/update/remove", () => {
  it("maps an unknown id to NOT_FOUND on get", async () => {
    const { service } = harness();

    await expect(service.get("missing")).rejects.toMatchObject({ code: "NOT_FOUND" });
  });

  it("updates the persona in the store and swaps the registration (unregister then register)", async () => {
    const { service, store, registry } = harness();
    await service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" });
    const before = await store.get("mine");

    const view = await service.update("mine", { persona: "P2" });

    expect(view.persona).toBe("P2");
    const updated = await store.get("mine");
    expect(updated.persona).toBe("P2");
    expect(updated.updateTime.getTime()).toBeGreaterThanOrEqual(before.updateTime.getTime());
    expect(registry.events).toEqual([
      { action: "register", id: "mine" },
      { action: "unregister", id: "mine" },
      { action: "register", id: "mine" },
    ]);
    expect(revisionPrefix(registry.active.get("mine"))).toBe("P2");
  });

  it("retires the old revision on update while a running agent keeps its reference", async () => {
    const { service, registry } = harness();
    await service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" });
    const runningCtx = {} as unknown as Context;
    await (await service.compose("mine")).setup(runningCtx);
    const runningRevision = registry.bindings.get(runningCtx);

    await service.update("mine", { persona: "P2" });

    // The old revision is retired but retained for the running agent...
    expect(runningRevision?.retired).toBe(true);
    expect(registry.bindings.get(runningCtx)).toBe(runningRevision);
    expect(revisionPrefix(runningRevision)).toBe("P1");
    // ...while a fresh agent binds to the new revision.
    const freshCtx = {} as unknown as Context;
    await (await service.compose("mine")).setup(freshCtx);
    expect(revisionPrefix(registry.bindings.get(freshCtx))).toBe("P2");
  });

  it("re-registers the definition when only displayName changes", async () => {
    const { service, store, registry } = harness();
    await service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" });

    const view = await service.update("mine", { displayName: "Renamed" });

    expect(view.displayName).toBe("Renamed");
    expect((await store.get("mine")).displayName).toBe("Renamed");
    expect(registry.register).toHaveBeenCalledTimes(2);
    expect(registry.register.mock.calls[1][0].name).toBe("Renamed");
  });

  it("treats an empty patch as a read (no re-registration)", async () => {
    const { service, registry } = harness();
    await service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" });

    const view = await service.update("mine", {});

    expect(view.persona).toBe("P1");
    expect(registry.register).toHaveBeenCalledOnce();
  });

  it("maps an unknown update target to NOT_FOUND", async () => {
    const { service } = harness();

    await expect(service.update("missing", { persona: "P2" })).rejects.toMatchObject({
      code: "NOT_FOUND",
    });
  });

  it("lists authored presets narrowed to one role pool", async () => {
    const { service } = harness();
    await service.create({ id: "p1", template: "demo-tools", role: "player", persona: "a" });
    await service.create({ id: "p2", template: "demo-tools", role: "planner", persona: "b" });

    const all = await service.list();
    expect(all.map((view) => view.id).sort()).toEqual(["p1", "p2"]);
    expect((await service.list("player")).map((view) => view.id)).toEqual(["p1"]);
    expect((await service.list("planner")).map((view) => view.id)).toEqual(["p2"]);
  });

  it("removes the store record and releases the registration", async () => {
    const { service, store, registry } = harness();
    await service.create({ id: "mine", template: "demo-tools", role: "planner", persona: "P1" });

    await service.remove("mine");

    expect(await store.list()).toEqual([]);
    expect(registry.events).toEqual([
      { action: "register", id: "mine" },
      { action: "unregister", id: "mine" },
    ]);
    expect(registry.active.has("mine")).toBe(false);
    await expect(service.get("mine")).rejects.toMatchObject({ code: "NOT_FOUND" });
  });

  it("maps an unknown remove target to NOT_FOUND without touching the registry", async () => {
    const { service, registry } = harness();

    await expect(service.remove("missing")).rejects.toMatchObject({ code: "NOT_FOUND" });
    expect(registry.events).toEqual([]);
  });
});

describe("engine restore/dispose", () => {
  it("materializes every stored record into the registry at boot", async () => {
    const { engine, store, registry } = harness();
    await storeRecord(store, { id: "mine", persona: "P1" });

    await engine.restore();

    expect(registry.register).toHaveBeenCalledOnce();
    expect(registry.active.has("mine")).toBe(true);
    expect(prefixOf(registry.active.get("mine")?.definition.plugins[0])).toBe("P1");
  });

  it("skips a stored record whose template is undeclared rather than failing startup", async () => {
    const { engine, store, registry } = harness();
    await storeRecord(store, { id: "ghost", template: "ghost" });

    await expect(engine.restore()).resolves.toBeUndefined();
    expect(registry.register).not.toHaveBeenCalled();
  });

  it("leaves a broken record in the roster (activation failure does not fail boot)", async () => {
    const { engine, store, registry } = harness();
    registry.broken.set("bad", "corrupted");
    await storeRecord(store, { id: "bad", persona: "P1" });

    await engine.restore();

    expect(registry.active.has("bad")).toBe(true);
  });

  it("unregisters every owned registration on dispose", async () => {
    const { engine, store, registry } = harness();
    await storeRecord(store, { id: "mine", persona: "P1" });
    await engine.restore();

    await engine.dispose();

    expect(registry.active.size).toBe(0);
    expect(registry.events).toEqual([
      { action: "register", id: "mine" },
      { action: "unregister", id: "mine" },
    ]);
  });
});

describe("Config", () => {
  it("validates template declarations and caller-declared row rules, defaulting omitted maps empty", () => {
    const raw: unknown = {
      storage: "memory",
      templates: {
        "demo-tools": {
          name: "Demo",
          description: "persona + demo_echo 工具行模板",
          plugins: [{ name: PERSONA_ROW_NAME, config: { prefix: "x" } }],
        },
      },
      templateRules: {
        player: {
          required: ["@dominion/dsh-saolei"],
          forbidden: ["@dominion/dsh-memory"],
        },
      },
    };
    const config = Config(raw as PresetAuthoringConfig);

    expect(config.templates).toEqual({
      "demo-tools": {
        name: "Demo",
        description: "persona + demo_echo 工具行模板",
        plugins: [{ name: PERSONA_ROW_NAME, config: { prefix: "x" } }],
      },
    });
    expect(config.templateRules).toEqual({
      player: { required: ["@dominion/dsh-saolei"], forbidden: ["@dominion/dsh-memory"] },
    });
    // Omitted maps default to empty: generic deployments declare neither.
    expect(Config({ storage: "memory" }).templates).toEqual({});
    expect(Config({ storage: "memory" }).templateRules).toEqual({});
  });

  it("defaults a one-sided rule table's omitted half to an empty list", () => {
    const raw: unknown = {
      storage: "memory",
      templateRules: {
        player: { required: ["@dominion/dsh-saolei"] },
        planner: { forbidden: ["@dominion/dsh-memory"] },
      },
    };
    const config = Config(raw as PresetAuthoringConfig);

    expect(config.templateRules).toEqual({
      player: { required: ["@dominion/dsh-saolei"], forbidden: [] },
      planner: { required: [], forbidden: ["@dominion/dsh-memory"] },
    });
  });
});

describe("apply", () => {
  it("provides the service under ctx.presetAuthoring and holds its registry disposer", async () => {
    const { apply, PresetAuthoringError: ExportedError } = await import("./index.js");
    const register = vi.fn(async (_definition: PresetDefinition) => async () => undefined);
    const resolve = vi.fn(async (id: string): Promise<AgentPreset> => ({ id }));
    const mount = vi.fn(async (_ctx: Context, id: string): Promise<AgentPreset> => ({ id }));
    const effect = vi.fn(() => () => undefined);
    const provide = vi.fn();

    await apply(
      {
        agentPresets: { register, resolve, mount },
        effect,
        provide,
        logger: { warn: vi.fn() },
      } as never,
      { storage: "memory" },
    );

    expect(effect).toHaveBeenCalledOnce();
    expect(provide).toHaveBeenCalledOnce();
    expect(provide.mock.calls[0][0]).toBe("presetAuthoring");
    // The re-exported error type carries the stable code surface.
    expect(new ExportedError("NOT_FOUND", "x").code).toBe("NOT_FOUND");
  });
});
