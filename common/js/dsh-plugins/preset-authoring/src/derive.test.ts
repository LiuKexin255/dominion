import { describe, expect, it } from "vitest";

import {
  deriveComposition,
  PERSONA_ROW_NAME,
  validateTemplateRows,
} from "./derive.js";

import type {
  CompositionRow,
  TemplateDeclaration,
  TemplateRowRules,
} from "./derive.js";
import type { PresetRecord } from "./store.js";

/** A two-row template declaration mirroring a hosting composition's preset row. */
const TEMPLATE: TemplateDeclaration = {
  name: "Demo Tools",
  description: "persona + demo_echo 工具行模板",
  plugins: [
    { id: "persona", name: PERSONA_ROW_NAME, config: { prefix: "placeholder" } },
    { id: "demo-echo", name: "@dominion/dsh-demo-echo" },
  ],
};

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

function prefixOf(row: CompositionRow | undefined): unknown {
  return (row?.config as { prefix?: unknown } | undefined)?.prefix;
}

describe("deriveComposition", () => {
  it("patches the persona row prefix, preserves sibling rows, and carries display metadata", () => {
    const derived = deriveComposition(
      record({ persona: "P1", displayName: "Mine" }),
      TEMPLATE,
    );

    expect(derived.id).toBe("mine");
    expect(derived.name).toBe("Mine");
    expect(derived.description).toBe("persona + demo_echo 工具行模板");
    expect(derived.plugins).toHaveLength(2);
    expect(derived.plugins[0]?.name).toBe(PERSONA_ROW_NAME);
    expect(prefixOf(derived.plugins[0])).toBe("P1");
    expect(derived.plugins[1]?.name).toBe("@dominion/dsh-demo-echo");
  });

  it("keeps the template persona prefix (the role default base) when the record persona is empty", () => {
    const derived = deriveComposition(record({ persona: "" }), TEMPLATE);

    expect(prefixOf(derived.plugins[0])).toBe("placeholder");
  });

  it("omits the registry name when the record has no displayName", () => {
    const derived = deriveComposition(record({ persona: "P1" }), TEMPLATE);

    expect("name" in derived).toBe(false);
    // The template supplies the description regardless of displayName.
    expect(derived.description).toBe("persona + demo_echo 工具行模板");
  });

  it("never mutates the injected template snapshot", () => {
    deriveComposition(record({ persona: "P1" }), TEMPLATE);

    expect(prefixOf(TEMPLATE.plugins[0])).toBe("placeholder");
  });

  it("is idempotent: the same record derives content-equivalent rows", () => {
    const stored = record({ persona: "P1" });

    const first = deriveComposition(stored, TEMPLATE);
    const second = deriveComposition(stored, TEMPLATE);

    expect(first.plugins).toEqual(second.plugins);
  });

  it.each([
    { label: "no persona row", plugins: [{ name: "@dominion/dsh-saolei" }] },
    {
      label: "two persona rows",
      plugins: [
        { id: "a", name: PERSONA_ROW_NAME, config: { prefix: "x" } },
        { id: "b", name: PERSONA_ROW_NAME, config: { prefix: "y" } },
      ],
    },
  ])("rejects a template convention violation ($label) with INVALID_ARGUMENT", ({ plugins }) => {
    expect(() =>
      deriveComposition(record({ persona: "P1" }), { plugins }),
    ).toThrowError(
      expect.objectContaining({
        code: "INVALID_ARGUMENT",
        message: expect.stringContaining(PERSONA_ROW_NAME),
      }),
    );
  });
});

describe("validateTemplateRows", () => {
  const rules: TemplateRowRules = {
    required: ["@dominion/dsh-demo-echo"],
    forbidden: ["@dominion/dsh-memory"],
  };

  it("accepts a template carrying exactly the required row and none of the forbidden ones", () => {
    expect(() => validateTemplateRows("demo-tools", TEMPLATE.plugins, rules)).not.toThrow();
  });

  it("rejects a missing required row with INVALID_ARGUMENT", () => {
    const rows: CompositionRow[] = [
      { name: PERSONA_ROW_NAME, config: { prefix: "x" } },
      { name: "@dominion/dsh-memory" },
    ];

    expect(() => validateTemplateRows("demo-tools", rows, rules)).toThrowError(
      expect.objectContaining({ code: "INVALID_ARGUMENT", message: expect.stringContaining("exactly one") }),
    );
  });

  it("rejects a duplicated required row and a forbidden row", () => {
    expect(() =>
      validateTemplateRows(
        "demo-tools",
        [{ name: "@dominion/dsh-demo-echo" }, { name: "@dominion/dsh-demo-echo" }],
        rules,
      ),
    ).toThrowError(expect.objectContaining({ code: "INVALID_ARGUMENT" }));

    expect(() =>
      validateTemplateRows(
        "demo-tools",
        [{ name: "@dominion/dsh-demo-echo" }, { name: "@dominion/dsh-memory" }],
        rules,
      ),
    ).toThrowError(
      expect.objectContaining({ code: "INVALID_ARGUMENT", message: expect.stringContaining("must not contain") }),
    );
  });

  it("treats an omitted rule half as an empty list (one-sided rule tables)", () => {
    // A host YAML value reaches the validator untyped; the declared rule type
    // mirrors the Config-validated output shape, hence the widening cast.
    const requiredOnly = { required: ["@dominion/dsh-demo-echo"] } as unknown as TemplateRowRules;
    const forbiddenOnly = { forbidden: ["@dominion/dsh-memory"] } as unknown as TemplateRowRules;

    expect(() => validateTemplateRows("demo-tools", TEMPLATE.plugins, requiredOnly)).not.toThrow();
    expect(() => validateTemplateRows("demo-tools", TEMPLATE.plugins, forbiddenOnly)).not.toThrow();
  });
});
