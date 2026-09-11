import { describe, expect, it, vi } from "vitest";

import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { dump, load } from "js-yaml";

import {
  COMPOSITION_FILE_NAME,
  deriveComposition,
  nodeDeriveFs,
  PERSONA_ROW_NAME,
  validateTemplateRows,
} from "./derive.js";

import type { AgentPreset } from "@deepseek-ai/dsh-agent-presets";
import type { DeriveFs, TemplateRowRules } from "./derive.js";
import type { PresetRecord } from "./store.js";

/** A two-row composition mirroring the pool template shape. */
const TEMPLATE_COMPOSITION = [
  { id: "persona", name: PERSONA_ROW_NAME, config: { text: "placeholder" } },
  { id: "saolei", name: "@dominion/dsh-saolei" },
];

function dumpRows(rows: unknown[]): string {
  return dump(rows, { lineWidth: -1 });
}

function parseRows(text: string): Array<{ name?: string; config?: { text?: string } }> {
  return load(text) as Array<{ name?: string; config?: { text?: string } }>;
}

function record(overrides: Partial<PresetRecord> = {}): PresetRecord {
  const now = new Date("2026-09-11T00:00:00Z");
  return {
    id: "mine",
    template: "demo-tools",
    role: "player",
    persona: "P1",
    createTime: now,
    updateTime: now,
    ...overrides,
  };
}

function template(overrides: Partial<AgentPreset> = {}): AgentPreset {
  return {
    id: "demo-tools",
    trust: "system",
    path: "/templates/demo-tools/agent.cordis.yml",
    ...overrides,
  };
}

/** An fs double that hands out a distinct mock directory per derivation. */
function mockFs(compositionText: string) {
  let dirSeq = 0;
  const readTextFile = vi.fn<(path: string) => Promise<string>>(async () => compositionText);
  const createTempDir = vi.fn<(prefix: string) => Promise<string>>(
    async (prefix) => `${prefix}${++dirSeq}`,
  );
  const writeTextFile = vi.fn<(path: string, data: string) => Promise<void>>(async () => undefined);
  const fs: DeriveFs = { readTextFile, createTempDir, writeTextFile };
  return { fs, readTextFile, createTempDir, writeTextFile };
}

describe("deriveComposition", () => {
  it("patches the persona row, preserves sibling rows, and returns the synthesized preset", async () => {
    const { fs, readTextFile, createTempDir, writeTextFile } = mockFs(dumpRows(TEMPLATE_COMPOSITION));
    const poolTemplate = template();

    const derived = await deriveComposition(record({ persona: "P1" }), poolTemplate, fs);

    expect(readTextFile).toHaveBeenCalledWith(poolTemplate.path);
    const dir = `${createTempDir.mock.calls[0]?.[0] as string}1`;
    expect(createTempDir).toHaveBeenCalledWith(expect.stringContaining(tmpdir()));
    expect(derived).toEqual({ id: "mine", trust: "user", path: join(dir, COMPOSITION_FILE_NAME) });

    expect(writeTextFile).toHaveBeenCalledOnce();
    const [path, data] = writeTextFile.mock.calls[0] as [string, string];
    expect(path).toBe(derived.path);
    const written = parseRows(data);
    expect(written).toHaveLength(2);
    expect(written[0]?.name).toBe(PERSONA_ROW_NAME);
    expect(written[0]?.config?.text).toBe("P1");
    expect(written[1]?.name).toBe("@dominion/dsh-saolei");
  });

  it("keeps the template persona row (the role default base) when the record persona is empty", async () => {
    const { fs, writeTextFile } = mockFs(dumpRows(TEMPLATE_COMPOSITION));

    await deriveComposition(record({ persona: "" }), template(), fs);

    expect(writeTextFile).toHaveBeenCalledOnce();
    const written = parseRows(writeTextFile.mock.calls[0]?.[1] as string);
    expect(written[0]?.config?.text).toBe("placeholder");
  });

  it("carries a persona-less template untouched when the record persona is empty", async () => {
    const { fs, writeTextFile } = mockFs(
      dumpRows([{ id: "saolei", name: "@dominion/dsh-saolei" }]),
    );

    await deriveComposition(record({ persona: "" }), template(), fs);

    const written = parseRows(writeTextFile.mock.calls[0]?.[1] as string);
    expect(written).toEqual([{ id: "saolei", name: "@dominion/dsh-saolei" }]);
  });

  it("is idempotent: the same record derives byte-equivalent compositions at distinct paths", async () => {
    const { fs, writeTextFile } = mockFs(dumpRows(TEMPLATE_COMPOSITION));
    const stored = record({ persona: "P1" });

    const first = await deriveComposition(stored, template(), fs);
    const second = await deriveComposition(stored, template(), fs);

    expect(first.path).not.toBe(second.path);
    const [firstPath, firstData] = writeTextFile.mock.calls[0] as [string, string];
    const [secondPath, secondData] = writeTextFile.mock.calls[1] as [string, string];
    expect(firstPath).toBe(first.path);
    expect(secondPath).toBe(second.path);
    // Same rows, same serialization: a rebuild is content-equivalent.
    expect(firstData).toBe(secondData);
  });

  it.each([
    { label: "no persona row", rows: [{ id: "saolei", name: "@dominion/dsh-saolei" }] },
    {
      label: "two persona rows",
      rows: [
        { id: "a", name: PERSONA_ROW_NAME, config: { text: "x" } },
        { id: "b", name: PERSONA_ROW_NAME, config: { text: "y" } },
      ],
    },
  ])("rejects a template convention violation ($label) with INVALID_ARGUMENT", async ({ rows }) => {
    const { fs, writeTextFile } = mockFs(dumpRows(rows));

    await expect(
      deriveComposition(record({ persona: "P1" }), template(), fs),
    ).rejects.toMatchObject({ code: "INVALID_ARGUMENT", message: expect.stringContaining(PERSONA_ROW_NAME) });
    expect(writeTextFile).not.toHaveBeenCalled();
  });

  it("rejects a non-list composition with INTERNAL (fail loud)", async () => {
    const { fs } = mockFs("{ not: [a list");

    await expect(deriveComposition(record(), template(), fs)).rejects.toMatchObject({
      code: "INTERNAL",
      message: expect.stringContaining("does not parse"),
    });
  });
});

describe("deriveComposition over the real filesystem", () => {
  it("writes agent.cordis.yml under a fresh system temp directory", async () => {
    const base = await mkdtemp(join(tmpdir(), "preset-derive-"));
    try {
      const templatePath = join(base, "agent.cordis.yml");
      await writeFile(templatePath, dumpRows(TEMPLATE_COMPOSITION), "utf8");

      const derived = await deriveComposition(record({ persona: "P-real" }), template({ path: templatePath }), nodeDeriveFs());

      expect(derived.trust).toBe("user");
      expect(derived.path.startsWith(tmpdir())).toBe(true);
      expect(derived.path.endsWith(COMPOSITION_FILE_NAME)).toBe(true);
      const written = parseRows(await readFile(derived.path, "utf8"));
      expect(written[0]?.config?.text).toBe("P-real");
      await rm(join(derived.path, ".."), { recursive: true, force: true });
    } finally {
      await rm(base, { recursive: true, force: true });
    }
  });
});

describe("validateTemplateRows", () => {
  const rules: TemplateRowRules = {
    required: ["@dominion/dsh-saolei"],
    forbidden: ["@dominion/dsh-memory/preset-row"],
  };

  it("accepts a template carrying exactly the required row and none of the forbidden ones", async () => {
    const { fs } = mockFs(dumpRows(TEMPLATE_COMPOSITION));

    await expect(validateTemplateRows(template(), rules, fs)).resolves.toBeUndefined();
  });

  it("rejects a missing required row with INVALID_ARGUMENT", async () => {
    const { fs } = mockFs(
      dumpRows([
        { id: "persona", name: PERSONA_ROW_NAME, config: { text: "x" } },
        { id: "memory", name: "@dominion/dsh-memory/preset-row" },
      ]),
    );

    await expect(validateTemplateRows(template(), rules, fs)).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
      message: expect.stringContaining("exactly one"),
    });
  });

  it("rejects a duplicated required row and a forbidden row", async () => {
    const duplicated = mockFs(
      dumpRows([
        { id: "saolei-a", name: "@dominion/dsh-saolei" },
        { id: "saolei-b", name: "@dominion/dsh-saolei" },
      ]),
    );
    await expect(validateTemplateRows(template(), rules, duplicated.fs)).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
    });

    const forbidden = mockFs(
      dumpRows([
        { id: "saolei", name: "@dominion/dsh-saolei" },
        { id: "memory", name: "@dominion/dsh-memory/preset-row" },
      ]),
    );
    await expect(validateTemplateRows(template(), rules, forbidden.fs)).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
      message: expect.stringContaining("must not contain"),
    });
  });

  it("treats an omitted rule half as an empty list (one-sided rule tables)", async () => {
    const { fs } = mockFs(dumpRows(TEMPLATE_COMPOSITION));
    // A host YAML value reaches the validator untyped; the declared rule type
    // mirrors the Config-validated output shape, hence the widening cast.
    const requiredOnly = { required: ["@dominion/dsh-saolei"] } as unknown as TemplateRowRules;
    const forbiddenOnly = {
      forbidden: ["@dominion/dsh-memory/preset-row"],
    } as unknown as TemplateRowRules;

    await expect(validateTemplateRows(template(), requiredOnly, fs)).resolves.toBeUndefined();
    await expect(validateTemplateRows(template(), forbiddenOnly, fs)).resolves.toBeUndefined();
  });
});
