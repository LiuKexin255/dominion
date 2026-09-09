import { describe, expect, it, vi } from "vitest";

import { PresetExistsError, UnknownPresetError } from "@deepseek-ai/dsh-agent-presets";
import { dump, load } from "js-yaml";

import {
  PERSONA_ROW_NAME,
  materializeCopy,
  nodeMaterializeFs,
  removeMaterialization,
  updateMaterialization,
} from "./materialize.js";

import type { MaterializeFs, RosterSeam } from "./materialize.js";

/** A two-row composition mirroring the demo-tools template shape. */
const TEMPLATE_COMPOSITION = [
  { id: "persona", name: PERSONA_ROW_NAME, config: { text: "placeholder" } },
  { id: "demo-echo", name: "@dominion/dsh-demo-echo" },
];

function dumpRows(rows: unknown[]): string {
  return dump(rows, { lineWidth: -1 });
}

function parseRows(text: string): Array<{ name?: string; config?: { text?: string } }> {
  return load(text) as Array<{ name?: string; config?: { text?: string } }>;
}

function mockFs(compositionText: string) {
  const readTextFile = vi.fn().mockResolvedValue(compositionText);
  const writeTextFileAtomic = vi.fn().mockResolvedValue(undefined);
  const removeDeep = vi.fn().mockResolvedValue(undefined);
  const fs: MaterializeFs = { readTextFile, writeTextFileAtomic, removeDeep };
  return { fs, readTextFile, writeTextFileAtomic, removeDeep };
}

function mockRoster() {
  const resolve = vi.fn().mockResolvedValue({
    id: "demo-tools",
    trust: "system",
    path: "/templates/demo-tools/agent.cordis.yml",
  });
  const mount = vi.fn();
  const copy = vi.fn().mockResolvedValue(undefined);
  const remove = vi.fn().mockResolvedValue(undefined);
  const roster = { resolve, mount, copy, remove } as unknown as RosterSeam;
  return { roster, resolve, mount, copy, remove };
}

/** One preset resolve answer for a template, then one for the fresh copy. */
function resolveTemplateThenCopy(handle: ReturnType<typeof vi.fn>, copyPath: string): void {
  handle.mockResolvedValueOnce({
    id: "demo-tools",
    trust: "system",
    path: "/templates/demo-tools/agent.cordis.yml",
  });
  handle.mockResolvedValueOnce({ id: "mine", trust: "user", path: copyPath });
}

describe("materializeCopy", () => {
  it("copies then patches the persona row, preserving sibling rows", async () => {
    const { roster, copy, resolve } = mockRoster();
    resolveTemplateThenCopy(resolve, "/writable/mine/agent.cordis.yml");
    const { fs, writeTextFileAtomic } = mockFs(dumpRows(TEMPLATE_COMPOSITION));

    await materializeCopy({ roster, fs }, { id: "mine", template: "demo-tools", persona: "P1" });

    // The display name rides the roster copy's third parameter (contract §4).
    expect(copy).toHaveBeenCalledOnce();
    expect(copy).toHaveBeenCalledWith("demo-tools", "mine", "mine");

    expect(writeTextFileAtomic).toHaveBeenCalledOnce();
    const [path, data] = writeTextFileAtomic.mock.calls[0];
    expect(path).toBe("/writable/mine/agent.cordis.yml");
    const written = parseRows(data as string);
    expect(written).toHaveLength(2);
    expect(written[0]?.name).toBe(PERSONA_ROW_NAME);
    expect(written[0]?.config?.text).toBe("P1");
    expect(written[1]?.name).toBe("@dominion/dsh-demo-echo");
  });

  it("passes the display name through to the roster copy when provided", async () => {
    const { roster, copy, resolve } = mockRoster();
    resolveTemplateThenCopy(resolve, "/writable/mine/agent.cordis.yml");
    const { fs } = mockFs(dumpRows(TEMPLATE_COMPOSITION));

    await materializeCopy(
      { roster, fs },
      { id: "mine", template: "demo-tools", persona: "P1", displayName: "Mine" },
    );

    expect(copy).toHaveBeenCalledWith("demo-tools", "mine", "Mine");
  });

  it("rejects an unknown template with INVALID_ARGUMENT naming the available ids", async () => {
    const { roster, copy } = mockRoster();
    const resolve = vi
      .fn()
      .mockRejectedValue(new UnknownPresetError("nope", ["demo-standard", "demo-tools"]));
    const { fs, writeTextFileAtomic } = mockFs(dumpRows(TEMPLATE_COMPOSITION));

    await expect(
      materializeCopy({ roster: { ...roster, resolve }, fs }, { id: "mine", template: "nope", persona: "P1" }),
    ).rejects.toMatchObject({ code: "INVALID_ARGUMENT", message: expect.stringContaining("demo-tools") });

    expect(copy).not.toHaveBeenCalled();
    expect(writeTextFileAtomic).not.toHaveBeenCalled();
  });

  it("rejects a broken template with INVALID_ARGUMENT before copying", async () => {
    const { roster, copy } = mockRoster();
    const resolve = vi.fn().mockResolvedValue({
      id: "demo-tools",
      trust: "system",
      path: "/templates/demo-tools/agent.cordis.yml",
      broken: "unparsable YAML",
    });
    const { fs } = mockFs(dumpRows(TEMPLATE_COMPOSITION));

    await expect(
      materializeCopy({ roster: { ...roster, resolve }, fs }, { id: "mine", template: "demo-tools", persona: "P1" }),
    ).rejects.toMatchObject({ code: "INVALID_ARGUMENT", message: expect.stringContaining("unparsable") });

    expect(copy).not.toHaveBeenCalled();
  });

  it("rejects an id outside the preset grammar with INVALID_ARGUMENT", async () => {
    const { roster, copy } = mockRoster();
    const { fs } = mockFs(dumpRows(TEMPLATE_COMPOSITION));

    await expect(
      materializeCopy({ roster, fs }, { id: "../escape", template: "demo-tools", persona: "P1" }),
    ).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
    expect(copy).not.toHaveBeenCalled();
  });

  it("maps a roster copy existence refusal to ALREADY_EXISTS", async () => {
    const { roster, copy, resolve } = mockRoster();
    resolveTemplateThenCopy(resolve, "/writable/mine/agent.cordis.yml");
    copy.mockRejectedValue(new PresetExistsError("mine"));
    const { fs } = mockFs(dumpRows(TEMPLATE_COMPOSITION));

    await expect(
      materializeCopy({ roster, fs }, { id: "mine", template: "demo-tools", persona: "P1" }),
    ).rejects.toMatchObject({ code: "ALREADY_EXISTS" });
  });

  it("maps an unexpected roster failure on the template resolve to INTERNAL, keeping the cause", async () => {
    const { roster, copy } = mockRoster();
    const boom = new Error("fs exploded");
    const resolve = vi.fn().mockRejectedValue(boom);
    const { fs } = mockFs(dumpRows(TEMPLATE_COMPOSITION));

    const err = await materializeCopy(
      { roster: { ...roster, resolve }, fs },
      { id: "mine", template: "demo-tools", persona: "P1" },
    ).then(
      () => undefined,
      (e: unknown) => e,
    );

    expect(err).toMatchObject({ code: "INTERNAL", message: expect.stringContaining("fs exploded") });
    expect((err as { cause?: unknown }).cause).toBe(boom);
    expect(copy).not.toHaveBeenCalled();
  });

  it("maps an unexpected roster failure on the copy step to INTERNAL", async () => {
    const { roster, copy, remove, resolve } = mockRoster();
    resolveTemplateThenCopy(resolve, "/writable/mine/agent.cordis.yml");
    copy.mockRejectedValue(new Error("copy exploded"));
    const { fs, removeDeep } = mockFs(dumpRows(TEMPLATE_COMPOSITION));

    await expect(
      materializeCopy({ roster, fs }, { id: "mine", template: "demo-tools", persona: "P1" }),
    ).rejects.toMatchObject({
      code: "INTERNAL",
      message: expect.stringContaining("copy exploded"),
    });
    // The roster copy rolls back its own half-made directory (its contract);
    // nothing landed, so the plugin performs no rollback of its own.
    expect(remove).not.toHaveBeenCalled();
    expect(removeDeep).not.toHaveBeenCalled();
  });

  it("rolls the copy directory back when the patch step fails", async () => {
    const { roster, remove, resolve } = mockRoster();
    resolveTemplateThenCopy(resolve, "/writable/mine/agent.cordis.yml");
    const { fs, writeTextFileAtomic } = mockFs(dumpRows(TEMPLATE_COMPOSITION));
    writeTextFileAtomic.mockRejectedValue(new Error("disk full"));

    await expect(
      materializeCopy({ roster, fs }, { id: "mine", template: "demo-tools", persona: "P1" }),
    ).rejects.toMatchObject({ code: "INTERNAL", message: expect.stringContaining("disk full") });

    // No half-materialized residue: the copied directory is removed.
    expect(remove).toHaveBeenCalledOnce();
    expect(remove).toHaveBeenCalledWith("mine");
  });

  it.each([
    { label: "no persona row", rows: [{ id: "demo-echo", name: "@dominion/dsh-demo-echo" }] },
    {
      label: "two persona rows",
      rows: [
        { id: "a", name: PERSONA_ROW_NAME, config: { text: "x" } },
        { id: "b", name: PERSONA_ROW_NAME, config: { text: "y" } },
      ],
    },
  ])("rejects a template convention violation ($label) with INVALID_ARGUMENT", async ({ rows }) => {
    const { roster, remove, resolve } = mockRoster();
    resolveTemplateThenCopy(resolve, "/writable/mine/agent.cordis.yml");
    const { fs } = mockFs(dumpRows(rows));

    await expect(
      materializeCopy({ roster, fs }, { id: "mine", template: "demo-tools", persona: "P1" }),
    ).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
    expect(remove).toHaveBeenCalledOnce();
  });
});

describe("updateMaterialization", () => {
  it("re-patches only the composition file for a persona update", async () => {
    const { roster, resolve } = mockRoster();
    resolve.mockResolvedValue({ id: "mine", trust: "user", path: "/writable/mine/agent.cordis.yml" });
    const { fs, writeTextFileAtomic } = mockFs(dumpRows(TEMPLATE_COMPOSITION));

    await updateMaterialization(
      { roster, fs },
      { id: "mine", template: "demo-tools", patch: { persona: "P2" } },
    );

    expect(writeTextFileAtomic).toHaveBeenCalledOnce();
    const [path, data] = writeTextFileAtomic.mock.calls[0];
    expect(path).toBe("/writable/mine/agent.cordis.yml");
    expect(parseRows(data as string)[0]?.config?.text).toBe("P2");
  });

  it("rewrites only preset.yml (name + template description) for a displayName update", async () => {
    const { roster, resolve } = mockRoster();
    resolve
      .mockResolvedValueOnce({ id: "mine", trust: "user", path: "/writable/mine/agent.cordis.yml" })
      .mockResolvedValueOnce({
        id: "demo-tools",
        trust: "system",
        path: "/templates/demo-tools/agent.cordis.yml",
        description: "persona + demo_echo 工具行模板",
      });
    const { fs, writeTextFileAtomic } = mockFs(dumpRows(TEMPLATE_COMPOSITION));

    await updateMaterialization(
      { roster, fs },
      { id: "mine", template: "demo-tools", patch: { displayName: "Mine" } },
    );

    expect(writeTextFileAtomic).toHaveBeenCalledOnce();
    const [path, data] = writeTextFileAtomic.mock.calls[0];
    expect(path).toBe("/writable/mine/preset.yml");
    expect(String(data)).toContain("name: Mine");
    expect(String(data)).toContain("persona + demo_echo 工具行模板");
  });

  it("fails loud as INTERNAL when the recorded copy is gone from the roster", async () => {
    const { roster } = mockRoster();
    const resolve = vi.fn().mockRejectedValue(new UnknownPresetError("mine", ["demo-standard"]));
    const { fs } = mockFs(dumpRows(TEMPLATE_COMPOSITION));

    await expect(
      updateMaterialization({ roster: { ...roster, resolve }, fs }, { id: "mine", template: "demo-tools", patch: { persona: "P2" } }),
    ).rejects.toMatchObject({ code: "INTERNAL" });
  });
});

describe("removeMaterialization", () => {
  it("deletes through the roster seam", async () => {
    const { roster, remove } = mockRoster();

    await removeMaterialization(roster, "mine");

    expect(remove).toHaveBeenCalledOnce();
    expect(remove).toHaveBeenCalledWith("mine");
  });

  it("maps a roster unknown-id refusal to NOT_FOUND", async () => {
    const { roster } = mockRoster();
    const remove = vi.fn().mockRejectedValue(new UnknownPresetError("gone", ["demo-standard"]));

    await expect(removeMaterialization({ ...roster, remove }, "gone")).rejects.toMatchObject({
      code: "NOT_FOUND",
    });
  });
});

describe("nodeMaterializeFs", () => {
  it("replaces files atomically via a temp file and rename with no temp residue", async () => {
    const { mkdtemp, readFile, readdir, rm, writeFile } = await import("node:fs/promises");
    const { tmpdir } = await import("node:os");
    const { join } = await import("node:path");
    const dir = await mkdtemp(join(tmpdir(), "materialize-fs-"));
    try {
      const file = join(dir, "data.yml");
      await writeFile(file, "old", "utf8");
      const fs = nodeMaterializeFs();

      await fs.writeTextFileAtomic(file, "new");

      await expect(readFile(file, "utf8")).resolves.toBe("new");
      await expect(readdir(dir)).resolves.toEqual(["data.yml"]);
    } finally {
      await rm(dir, { recursive: true, force: true });
    }
  });
});
