import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { readConfig } from "../src/index";
import { deepMerge } from "../src/merge";

// Config files live under `{DOMINION_CONFIG_DIR}/{block}/{key}`; tests build a
// real temp directory per test and point DOMINION_CONFIG_DIR at it via
// vi.stubEnv (no module interception — reliable under both the vitest CLI and
// Bazel, per style/javascript.md "Mock 约定" and specs/019 research §2/§6).

let configDir: string;

beforeEach(() => {
  configDir = fs.mkdtempSync(path.join(os.tmpdir(), "dominion-config-test-"));
  vi.stubEnv("DOMINION_CONFIG_DIR", configDir);
});

afterEach(() => {
  vi.unstubAllEnvs();
  fs.rmSync(configDir, { recursive: true, force: true });
});

function writeEntry(block: string, key: string, content: string): void {
  const dir = path.join(configDir, block);
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(path.join(dir, key), content, "utf8");
}

interface Greeting {
  message: string;
  times: number;
}

describe("deep merge semantics (data-model.md matrix)", () => {
  it("recursively merges nested plain objects (config keys override, defaults preserved)", () => {
    writeEntry("block", "entry", "a:\n  y: 3\n  z: 4\n");
    const merged = readConfig("block", "entry", { a: { x: 1, y: 2 } });
    expect(merged).toEqual({ a: { x: 1, y: 3, z: 4 } });
  });

  it("replaces scalars and arrays wholesale (arrays are not index-merged)", () => {
    writeEntry("block", "entry", "n: 2\nlist:\n  - 3\n  - 4\n  - 5\n");
    const merged = readConfig("block", "entry", {
      n: 1,
      list: [1, 2],
    });
    expect(merged).toEqual({ n: 2, list: [3, 4, 5] });
  });

  it("keeps default values for keys absent from the config", () => {
    writeEntry("block", "entry", "a: 9\n");
    const merged = readConfig("block", "entry", { a: 1, b: "keep" });
    expect(merged).toEqual({ a: 9, b: "keep" });
  });

  it("lets an explicit null override the default", () => {
    writeEntry("block", "entry", "a: null\nb: replaced\n");
    const merged = readConfig("block", "entry", { a: 1, b: "orig" });
    expect(merged).toEqual({ a: null, b: "replaced" });
  });

  it("replaces a default object when the config value is a scalar (type mismatch)", () => {
    writeEntry("block", "entry", "a: 5\n");
    const merged = readConfig("block", "entry", { a: { x: 1 } });
    expect(merged).toEqual({ a: 5 });
  });

  it("never lets an undefined source value override (merge layer)", () => {
    expect(deepMerge({ a: 1, b: 2 }, { a: undefined, b: 3 })).toEqual({
      a: 1,
      b: 3,
    });
  });

  it("merges the acceptance-scenario example from spec.md US3", () => {
    writeEntry("service_config", "key", '{"B": 222}\n');
    const merged = readConfig("service_config", "key", { A: "abc", B: 111 });
    expect(merged).toEqual({ A: "abc", B: 222 });
  });
});

describe("prototype pollution protection", () => {
  it("skips __proto__/constructor/prototype keys at the top level", () => {
    writeEntry(
      "block",
      "entry",
      "__proto__:\n  polluted: true\nconstructor:\n  polluted: true\nprototype:\n  polluted: true\nlegit: value\n",
    );
    const merged = readConfig("block", "entry", { legit: "default" });
    expect(merged).toEqual({ legit: "value" });
    expect(Object.keys(merged)).not.toContain("__proto__");
    expect(Object.keys(merged)).not.toContain("constructor");
    expect(Object.keys(merged)).not.toContain("prototype");
    expect(({} as Record<string, unknown>).polluted).toBeUndefined();
  });

  it("skips polluting keys inside nested objects", () => {
    writeEntry("block", "entry", "nested:\n  __proto__:\n    polluted: true\n  ok: 1\n");
    const merged = readConfig("block", "entry", { nested: { ok: 0 } });
    expect(merged).toEqual({ nested: { ok: 1 } });
    expect(({} as Record<string, unknown>).polluted).toBeUndefined();
  });
});

describe("defaults are not modified", () => {
  it("leaves the caller's defaults object untouched", () => {
    writeEntry("block", "entry", "a:\n  y: 3\nlist:\n  - 9\n");
    const defaults = { a: { x: 1, y: 2 }, list: [1, 2], n: 1 };
    const snapshot = JSON.stringify(defaults);
    const merged = readConfig("block", "entry", defaults);
    expect(JSON.stringify(defaults)).toBe(snapshot);
    // The result shares no references with defaults.
    (merged.a as { y: number }).y = 999;
    expect((defaults.a as { y: number }).y).toBe(2);
  });
});

describe("json and yaml entries", () => {
  it("parses a json-typed entry (JSON is a YAML subset)", () => {
    writeEntry("block", "entry", '{"B": 222}\n');
    const merged = readConfig("block", "entry", { A: "abc", B: 111 });
    expect(merged).toEqual({ A: "abc", B: 222 });
  });

  it("parses a yaml-typed entry equivalently", () => {
    writeEntry("block", "entry", "B: 222\n");
    const merged = readConfig("block", "entry", { A: "abc", B: 111 });
    expect(merged).toEqual({ A: "abc", B: 222 });
  });
});

describe("error cases", () => {
  it("throws when DOMINION_CONFIG_DIR is not set", () => {
    vi.stubEnv("DOMINION_CONFIG_DIR", undefined);
    expect(() =>
      readConfig("block", "entry", { a: 1 }),
    ).toThrowError(/DOMINION_CONFIG_DIR/);
  });

  it("throws when the config file is missing (block not selected by deploy.yaml)", () => {
    expect(() =>
      readConfig("unselected_block", "entry", { a: 1 }),
    ).toThrowError(/unselected_block\/entry/);
    expect(() =>
      readConfig("unselected_block", "entry", { a: 1 }),
    ).toThrowError(new RegExp(path.join(configDir, "unselected_block", "entry").replace(/\\/g, "\\\\")));
  });

  it("throws when the file content is not parseable", () => {
    writeEntry("block", "entry", "a: [unclosed\n");
    expect(() =>
      readConfig("block", "entry", { a: 1 }),
    ).toThrowError(/block\/entry/);
  });

  it("throws when the file content is not a mapping", () => {
    writeEntry("block", "entry", "just a string\n");
    expect(() =>
      readConfig("block", "entry", { a: 1 }),
    ).toThrowError(/block\/entry/);
  });

  it("does not leak the file content in error messages", () => {
    writeEntry("block", "entry", "secret-content-xyz: [unclosed\n");
    // The content is genuinely unparseable, so the read really throws.
    expect(() => readConfig("block", "entry", { a: 1 })).toThrowError(
      /block\/entry/,
    );
    let err: unknown;
    try {
      readConfig("block", "entry", { a: 1 });
    } catch (e) {
      err = e;
    }
    expect(err).toBeInstanceOf(Error);
    // js-yaml's message embeds a snippet of the offending line; the wrapper
    // must surface only the location, never file content.
    expect((err as Error).message).not.toContain("secret-content-xyz");
    expect((err as Error).message).toMatch(/line \d+, column \d+/);
  });
});

describe("multiple blocks and entries address independently", () => {
  it("reads each (block, key) pair without cross-talk", () => {
    writeEntry("service_config", "greeting", "message: hi from service_config\ntimes: 5\n");
    writeEntry("service_config", "limits", '{"maxConn": 100}\n');
    writeEntry("feature_flags", "greeting", "message: hi from feature_flags\n");

    const greeting: Greeting = readConfig<Greeting>("service_config", "greeting", {
      message: "hello",
      times: 1,
    });
    expect(greeting).toEqual({ message: "hi from service_config", times: 5 });

    const limits = readConfig("service_config", "limits", { maxConn: 10 });
    expect(limits).toEqual({ maxConn: 100 });

    const otherGreeting: Greeting = readConfig<Greeting>("feature_flags", "greeting", {
      message: "hello",
      times: 1,
    });
    expect(otherGreeting).toEqual({ message: "hi from feature_flags", times: 1 });
  });
});
