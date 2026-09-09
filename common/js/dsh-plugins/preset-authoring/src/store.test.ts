import { describe, expect, it } from "vitest";

import { MemoryPresetStore, PresetStoreError } from "./store.js";

import type { PresetRecord } from "./store.js";

function record(overrides: Partial<PresetRecord> = {}): PresetRecord {
  const now = new Date("2026-09-09T00:00:00Z");
  return {
    id: "my-preset",
    template: "player",
    role: "player",
    persona: "You are mine.",
    displayName: undefined,
    createTime: now,
    updateTime: now,
    ...overrides,
  };
}

describe("MemoryPresetStore", () => {
  it("round-trips a created record through get", async () => {
    const store = new MemoryPresetStore();

    await store.create(record());
    const got = await store.get("my-preset");

    expect(got.id).toBe("my-preset");
    expect(got.template).toBe("player");
    expect(got.role).toBe("player");
    expect(got.persona).toBe("You are mine.");
    expect(got.displayName).toBeUndefined();
  });

  it("rejects a duplicate id with ALREADY_EXISTS", async () => {
    const store = new MemoryPresetStore();
    await store.create(record());

    await expect(store.create(record())).rejects.toMatchObject({
      name: "PresetStoreError",
      code: "ALREADY_EXISTS",
    });
  });

  it("rejects reads of unknown ids with NOT_FOUND", async () => {
    const store = new MemoryPresetStore();

    await expect(store.get("missing")).rejects.toBeInstanceOf(PresetStoreError);
    await expect(store.get("missing")).rejects.toMatchObject({ code: "NOT_FOUND" });
  });

  it("lists every stored record", async () => {
    const store = new MemoryPresetStore();
    await store.create(record());
    await store.create(record({ id: "other", displayName: "Other" }));

    const listed = await store.list();

    expect(listed).toHaveLength(2);
    expect(listed.map((r) => r.id).sort()).toEqual(["my-preset", "other"]);
  });

  it("replaces a record on update and rejects updates of unknown ids", async () => {
    const store = new MemoryPresetStore();
    await store.create(record());

    const updated = record({ persona: "Changed.", updateTime: new Date("2026-09-09T01:00:00Z") });
    await store.update(updated);
    await expect(store.get("my-preset")).resolves.toMatchObject({ persona: "Changed." });
    await expect(store.update(record({ id: "missing" }))).rejects.toMatchObject({
      code: "NOT_FOUND",
    });
  });

  it("deletes a record and rejects deletes of unknown ids", async () => {
    const store = new MemoryPresetStore();
    await store.create(record());

    await store.remove("my-preset");
    await expect(store.get("my-preset")).rejects.toMatchObject({ code: "NOT_FOUND" });
    await expect(store.remove("my-preset")).rejects.toMatchObject({ code: "NOT_FOUND" });
  });

  it("keeps stored state unaffected by caller mutations of returned records", async () => {
    const store = new MemoryPresetStore();
    await store.create(record());

    const got = await store.get("my-preset");
    got.persona = "Mutated.";

    await expect(store.get("my-preset")).resolves.toMatchObject({ persona: "You are mine." });
  });
});
