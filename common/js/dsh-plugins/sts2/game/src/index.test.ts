/**
 * dsh-sts2 scaffold plugin tests: the lazy plugin shape
 * (specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md §3/§5).
 *
 * The assertions pin the row contract: `name` equals the composition row id,
 * `inject` is empty (nothing blocks the row from mounting) and `apply` is a
 * callable no-op. A real cordis Context is used (no module interception,
 * style/javascript.md Mock convention).
 */

import { Context } from "@deepseek-ai/cordis";
import { describe, expect, it } from "vitest";

import { apply, inject, name } from "./index.js";

describe("dsh-sts2 scaffold plugin", () => {
  it("uses the composition row id as the plugin name", () => {
    expect(name).toBe("sts2");
  });

  it("requires no service before mounting", () => {
    expect(inject).toEqual([]);
  });

  it("exposes a callable empty apply", () => {
    const ctx = new Context();
    expect(apply(ctx)).toBeUndefined();
  });
});
