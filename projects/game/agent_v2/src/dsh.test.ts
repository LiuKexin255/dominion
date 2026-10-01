import { afterEach, describe, expect, it, vi } from "vitest";
import { ENV_DOMINION_ARTIFACT_DIR, ENV_DOMINION_SECRET_DIR } from "@dominion/common-js-constants";
import { defaultLogger } from "@dominion/common-js-logs";
import * as fs from "node:fs";
// js-yaml v5 ships a real ESM build — named imports are statically resolved.
import { YAML11_SCHEMA, defineScalarTag, load } from "js-yaml";
import {
  bootDsh,
  cordisConfigPath,
  GLM_DEFAULT_BASE_URL,
  GLM_SECRET_FILE,
  OPENCODE_DEFAULT_BASE_URL,
  OPENCODE_SECRET_FILE,
} from "./dsh.js";
import type { DshBootDeps, DshContext } from "./dsh.js";
import type { EndpointResolver } from "@dominion/common-js-resolver";

/**
 * Fail-loud unit tests for the composition boot path
 * (specs/049-agent-v2-dsh-init/research.md D9;
 * specs/063-llm-reliability-opencode-go/research.md D13): endpoint precedence
 * (explicit `*_BASE_URL` > `*_LLM_TARGET` resolved > default), secret-file
 * token injection with zero token leakage in diagnostics (SC-004), and the
 * boot(binName, configPath, undefined, undefined, import.meta.url) call
 * shape — plus the composition manifest contract
 * (specs/070-dsh-upgrade/contracts/composition-manifest.md §2): the exact
 * 22-row set with the grpc-runtime infrastructure row, the saolei game
 * foundation row, the session-projection
 * row, the official agent-loop row,
 * the agent-preset registry and the pool-template snapshot injected through
 * the authoring row config, the saolei-loop flat capability-dependency list
 * (specs/074-dsh-grpc-runtime/contracts/team-assembly.md §2), the fail-loud
 * selection default, the trimmed system-prompt config, the subpath invariant
 * companion rows (specs/051-agent-v2-dsh-migration/research.md D5), and the
 * llm-glm + llm-opencode-go model catalogs.
 *
 * `boot`, the resolver, and the secret reader are injected as `vi.fn()`
 * doubles through the DshBootDeps seam; `process.exit` is spied so the
 * failure path can be asserted without killing the test runner
 * (style/javascript.md Mock convention).
 */

function fakeResolver(endpoints: string[]): EndpointResolver {
  return { resolve: vi.fn(async () => endpoints) };
}

function fakeBoot(ctx: DshContext) {
  // Optional params typed as supertypes of boot's real signature so the
  // mock stays assignable to `typeof boot` (contravariant parameters).
  return vi.fn(
    async (
      _binName: string,
      _configPath: string,
      _patches?: unknown,
      _prepare?: unknown,
      _anchor?: string,
    ) => ctx,
  );
}

const TOKEN = "glmtoken-do-not-leak-9f8e7d6c";
const OPENCODE_TOKEN = "oc-go-token-do-not-leak-1a2b3c";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("bootDsh", () => {
  it("prefers an already-set GLM_BASE_URL without contacting the resolver", async () => {
    const ctx = { marker: "ctx" } as unknown as DshContext;
    const boot = fakeBoot(ctx);
    const resolve = vi.fn(async () => ["10.0.0.9:8080"]);
    const exit = vi.spyOn(process, "exit").mockImplementation((() => undefined) as never);
    const env: Record<string, string | undefined> = {
      [ENV_DOMINION_ARTIFACT_DIR]: "/dominion/game/agent-v2",
      GLM_BASE_URL: "http://fake-llm:8080/v1",
      GLM_LLM_TARGET: "dominion:///game/fake-llm:8080",
      OPENCODE_BASE_URL: "http://opencode-fake:8080/v1",
      OPENCODE_LLM_TARGET: "dominion:///game/fake-llm:8080",
    };

    const result = await bootDsh({
      boot,
      resolver: { resolve },
      env,
      secretDir: "/tmp/secret",
      readSecretFile: () => TOKEN,
    });

    expect(result).toBe(ctx);
    expect(resolve).not.toHaveBeenCalled();
    expect(env.GLM_BASE_URL).toBe("http://fake-llm:8080/v1");
    expect(env.OPENCODE_BASE_URL).toBe("http://opencode-fake:8080/v1");
    expect(exit).not.toHaveBeenCalled();
  });

  it("resolves GLM_LLM_TARGET through Dominion discovery and appends the /v1 path", async () => {
    const ctx = { marker: "ctx" } as unknown as DshContext;
    const boot = fakeBoot(ctx);
    const resolve = vi.fn(async () => ["10.0.0.9:8080"]);
    const exit = vi.spyOn(process, "exit").mockImplementation((() => undefined) as never);
    const env: Record<string, string | undefined> = {
      [ENV_DOMINION_ARTIFACT_DIR]: "/dominion/game/agent-v2",
      GLM_LLM_TARGET: "dominion:///game/fake-llm:8080",
    };

    await bootDsh({
      boot,
      resolver: { resolve },
      env,
      secretDir: "/tmp/secret",
      readSecretFile: () => TOKEN,
    });

    expect(resolve).toHaveBeenCalledTimes(1);
    expect(resolve).toHaveBeenCalledWith("dominion:///game/fake-llm:8080");
    expect(env.GLM_BASE_URL).toBe("http://10.0.0.9:8080/v1");
    // No OPENCODE_LLM_TARGET set: the opencode-go row keeps its production
    // default without contacting the resolver.
    expect(env.OPENCODE_BASE_URL).toBe(OPENCODE_DEFAULT_BASE_URL);
  });

  it("falls back to the production GLM endpoint when neither env override is set", async () => {
    const ctx = { marker: "ctx" } as unknown as DshContext;
    const boot = fakeBoot(ctx);
    const resolve = vi.fn(async () => {
      throw new Error("resolver must not be contacted without GLM_LLM_TARGET");
    });
    const exit = vi.spyOn(process, "exit").mockImplementation((() => undefined) as never);
    const env: Record<string, string | undefined> = {
      [ENV_DOMINION_ARTIFACT_DIR]: "/dominion/game/agent-v2",
    };

    await bootDsh({
      boot,
      resolver: { resolve },
      env,
      secretDir: "/tmp/secret",
      readSecretFile: () => TOKEN,
    });

    expect(env.GLM_BASE_URL).toBe(GLM_DEFAULT_BASE_URL);
    expect(env.OPENCODE_BASE_URL).toBe(OPENCODE_DEFAULT_BASE_URL);
  });

  it("injects the trimmed token from the secret file as GLM_API_KEY", async () => {
    const ctx = { marker: "ctx" } as unknown as DshContext;
    const boot = fakeBoot(ctx);
    const exit = vi.spyOn(process, "exit").mockImplementation((() => undefined) as never);
    const env: Record<string, string | undefined> = {
      [ENV_DOMINION_ARTIFACT_DIR]: "/dominion/game/agent-v2",
    };
    const readSecretFile = vi.fn(() => `  ${TOKEN}\n`);

    await bootDsh({ boot, env, secretDir: "/tmp/secret", readSecretFile });

    expect(readSecretFile).toHaveBeenCalledWith(`/tmp/secret/${GLM_SECRET_FILE}`);
    expect(readSecretFile).toHaveBeenCalledWith(`/tmp/secret/${OPENCODE_SECRET_FILE}`);
    expect(env.GLM_API_KEY).toBe(TOKEN);
    expect(env.OPENCODE_API_KEY).toBe(TOKEN);
    expect(exit).not.toHaveBeenCalled();
  });

  it("prefers a pre-set GLM_API_KEY env without reading the secret file", async () => {
    const ctx = { marker: "ctx" } as unknown as DshContext;
    const boot = fakeBoot(ctx);
    const exit = vi.spyOn(process, "exit").mockImplementation((() => undefined) as never);
    const env: Record<string, string | undefined> = {
      [ENV_DOMINION_ARTIFACT_DIR]: "/dominion/game/agent-v2",
      GLM_API_KEY: `  ${TOKEN}  `,
      OPENCODE_API_KEY: `  ${OPENCODE_TOKEN}  `,
    };
    const readSecretFile = vi.fn(() => {
      throw new Error("secret file must not be read when both API keys are set");
    });

    await bootDsh({ boot, env, secretDir: "/tmp/secret", readSecretFile });

    expect(readSecretFile).not.toHaveBeenCalled();
    expect(env.GLM_API_KEY).toBe(TOKEN);
    expect(env.OPENCODE_API_KEY).toBe(OPENCODE_TOKEN);
    expect(exit).not.toHaveBeenCalled();
  });

  it("treats a whitespace-only GLM_API_KEY env as unset and reads the secret file", async () => {
    // Same empty-is-missing rule as the file path: a blank credential must
    // never satisfy the boot.
    const ctx = { marker: "ctx" } as unknown as DshContext;
    const boot = fakeBoot(ctx);
    const exit = vi.spyOn(process, "exit").mockImplementation((() => undefined) as never);
    const env: Record<string, string | undefined> = {
      [ENV_DOMINION_ARTIFACT_DIR]: "/dominion/game/agent-v2",
      GLM_API_KEY: "   ",
    };
    const readSecretFile = vi.fn(() => TOKEN);

    await bootDsh({ boot, env, secretDir: "/tmp/secret", readSecretFile });

    expect(readSecretFile).toHaveBeenCalledWith(`/tmp/secret/${GLM_SECRET_FILE}`);
    expect(env.GLM_API_KEY).toBe(TOKEN);
    expect(exit).not.toHaveBeenCalled();
  });

  it("uses $DOMINION_SECRET_DIR for the token file lookup", async () => {
    const ctx = { marker: "ctx" } as unknown as DshContext;
    const boot = fakeBoot(ctx);
    const exit = vi.spyOn(process, "exit").mockImplementation((() => undefined) as never);
    const readSecretFile = vi.fn(() => TOKEN);

    await bootDsh({
      boot,
      env: {
        [ENV_DOMINION_ARTIFACT_DIR]: "/dominion/game/agent-v2",
        [ENV_DOMINION_SECRET_DIR]: "/mnt/dominion/secret",
      },
      readSecretFile,
    });

    expect(readSecretFile).toHaveBeenCalledWith(`/mnt/dominion/secret/${GLM_SECRET_FILE}`);
  });

  it("tolerates a missing token file: warns and boots with GLM_API_KEY unset", async () => {
    // Three-level resolution terminal state (specs/049-agent-v2-dsh-init/
    // research.md D9): an absent secret never blocks the boot — the plugin
    // then sends requests without an Authorization header (glm-llm-plugin.md
    // §3 义务 6). The warning carries the env name and the resolved secret
    // file path, never any key value (SC-004).
    const boot = fakeBoot({} as DshContext);
    const exit = vi.spyOn(process, "exit").mockImplementation((() => undefined) as never);
    const warnSpy = vi.spyOn(defaultLogger(), "warn").mockImplementation(() => {});
    const env: Record<string, string | undefined> = {
      [ENV_DOMINION_ARTIFACT_DIR]: "/dominion/game/agent-v2",
      OPENCODE_API_KEY: OPENCODE_TOKEN,
    };
    const readSecretFile = vi.fn(() => {
      throw new Error(`ENOENT: no such file or directory, open '/mnt/dominion/secret/${GLM_SECRET_FILE}'`);
    });

    await bootDsh({ boot, env, secretDir: "/tmp/secret", readSecretFile });

    expect(boot).toHaveBeenCalledTimes(1);
    expect(exit).not.toHaveBeenCalled();
    expect(env.GLM_API_KEY).toBeUndefined();
    expect(warnSpy).toHaveBeenCalledTimes(1);
    const [message, attrs] = warnSpy.mock.calls[0] as [string, Record<string, string>];
    expect(message).toContain("GLM_API_KEY");
    expect(message + JSON.stringify(attrs)).toContain(`/tmp/secret/${GLM_SECRET_FILE}`);
    expect(message + JSON.stringify(attrs)).toContain("ENOENT");
    expect(message + JSON.stringify(attrs)).not.toContain(TOKEN);
  });

  it("tolerates an empty token file: warns and boots with GLM_API_KEY unset", async () => {
    const boot = fakeBoot({} as DshContext);
    const exit = vi.spyOn(process, "exit").mockImplementation((() => undefined) as never);
    const warnSpy = vi.spyOn(defaultLogger(), "warn").mockImplementation(() => {});
    const env: Record<string, string | undefined> = {
      [ENV_DOMINION_ARTIFACT_DIR]: "/dominion/game/agent-v2",
      OPENCODE_API_KEY: OPENCODE_TOKEN,
    };

    await bootDsh({ boot, env, secretDir: "/tmp/secret", readSecretFile: () => "   " });

    expect(boot).toHaveBeenCalledTimes(1);
    expect(exit).not.toHaveBeenCalled();
    expect(env.GLM_API_KEY).toBeUndefined();
    expect(warnSpy).toHaveBeenCalledTimes(1);
    const [message, attrs] = warnSpy.mock.calls[0] as [string, Record<string, string>];
    expect(message + JSON.stringify(attrs)).toContain(`/tmp/secret/${GLM_SECRET_FILE}`);
    expect(message + JSON.stringify(attrs)).toContain("absent or empty");
    expect(message + JSON.stringify(attrs)).not.toContain(TOKEN);
  });

  it("resolves OPENCODE_LLM_TARGET through Dominion discovery and appends the /v1 path", async () => {
    const ctx = { marker: "ctx" } as unknown as DshContext;
    const boot = fakeBoot(ctx);
    const resolve = vi.fn(async () => ["10.0.0.9:8080"]);
    const exit = vi.spyOn(process, "exit").mockImplementation((() => undefined) as never);
    const env: Record<string, string | undefined> = {
      [ENV_DOMINION_ARTIFACT_DIR]: "/dominion/game/agent-v2",
      OPENCODE_LLM_TARGET: "dominion:///game/fake-llm:8080",
    };

    await bootDsh({
      boot,
      resolver: { resolve },
      env,
      secretDir: "/tmp/secret",
      readSecretFile: () => TOKEN,
    });

    expect(resolve).toHaveBeenCalledTimes(1);
    expect(resolve).toHaveBeenCalledWith("dominion:///game/fake-llm:8080");
    expect(env.OPENCODE_BASE_URL).toBe("http://10.0.0.9:8080/v1");
    // GLM has neither override: its production default applies.
    expect(env.GLM_BASE_URL).toBe(GLM_DEFAULT_BASE_URL);
  });

  it("injects the trimmed token from the opencode-api-token file as OPENCODE_API_KEY", async () => {
    const ctx = { marker: "ctx" } as unknown as DshContext;
    const boot = fakeBoot(ctx);
    const exit = vi.spyOn(process, "exit").mockImplementation((() => undefined) as never);
    const env: Record<string, string | undefined> = {
      [ENV_DOMINION_ARTIFACT_DIR]: "/dominion/game/agent-v2",
      GLM_API_KEY: TOKEN,
    };
    const readSecretFile = vi.fn(() => `  ${OPENCODE_TOKEN}\n`);

    await bootDsh({ boot, env, secretDir: "/tmp/secret", readSecretFile });

    expect(readSecretFile).toHaveBeenCalledWith(`/tmp/secret/${OPENCODE_SECRET_FILE}`);
    expect(env.OPENCODE_API_KEY).toBe(OPENCODE_TOKEN);
    expect(exit).not.toHaveBeenCalled();
  });

  it("prefers a pre-set OPENCODE_API_KEY env without reading its secret file", async () => {
    const ctx = { marker: "ctx" } as unknown as DshContext;
    const boot = fakeBoot(ctx);
    const exit = vi.spyOn(process, "exit").mockImplementation((() => undefined) as never);
    const env: Record<string, string | undefined> = {
      [ENV_DOMINION_ARTIFACT_DIR]: "/dominion/game/agent-v2",
      GLM_API_KEY: TOKEN,
      OPENCODE_API_KEY: `  ${OPENCODE_TOKEN}  `,
    };
    const readSecretFile = vi.fn(() => {
      throw new Error("secret file must not be read when both API keys are set");
    });

    await bootDsh({ boot, env, secretDir: "/tmp/secret", readSecretFile });

    expect(readSecretFile).not.toHaveBeenCalled();
    expect(env.OPENCODE_API_KEY).toBe(OPENCODE_TOKEN);
    expect(exit).not.toHaveBeenCalled();
  });

  it("warns and boots with OPENCODE_API_KEY unset when its token file is absent", async () => {
    const boot = fakeBoot({} as DshContext);
    const exit = vi.spyOn(process, "exit").mockImplementation((() => undefined) as never);
    const warnSpy = vi.spyOn(defaultLogger(), "warn").mockImplementation(() => {});
    const env: Record<string, string | undefined> = {
      [ENV_DOMINION_ARTIFACT_DIR]: "/dominion/game/agent-v2",
      GLM_API_KEY: TOKEN,
    };
    const readSecretFile = vi.fn((file: string) => {
      throw new Error(`ENOENT: no such file or directory, open '${file}'`);
    });

    await bootDsh({ boot, env, secretDir: "/tmp/secret", readSecretFile });

    expect(boot).toHaveBeenCalledTimes(1);
    expect(exit).not.toHaveBeenCalled();
    expect(env.OPENCODE_API_KEY).toBeUndefined();
    expect(warnSpy).toHaveBeenCalledTimes(1);
    const [message, attrs] = warnSpy.mock.calls[0] as [string, Record<string, string>];
    expect(message).toContain("OPENCODE_API_KEY");
    expect(message + JSON.stringify(attrs)).toContain(`/tmp/secret/${OPENCODE_SECRET_FILE}`);
    expect(message + JSON.stringify(attrs)).toContain("ENOENT");
    expect(message + JSON.stringify(attrs)).not.toContain(OPENCODE_TOKEN);
  });

  it("fails loud when the resolver returns no endpoints for GLM_LLM_TARGET", async () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
    const boot = fakeBoot({} as DshContext);
    const exit = vi.spyOn(process, "exit").mockImplementation((() => undefined) as never);

    await bootDsh({
      boot,
      resolver: fakeResolver([]),
      env: {
        [ENV_DOMINION_ARTIFACT_DIR]: "/dominion/game/agent-v2",
        GLM_LLM_TARGET: "dominion:///game/fake-llm:8080",
      },
      secretDir: "/tmp/secret",
      readSecretFile: () => TOKEN,
    });

    expect(boot).not.toHaveBeenCalled();
    expect(consoleError).toHaveBeenCalledWith(expect.stringContaining("no endpoints"));
    expect(exit).toHaveBeenCalledWith(1);
  });

  it("fails loud with diagnostics and exit(1) when boot throws", async () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
    const boot = vi.fn(async () => {
      throw new Error("cordis.yml row 2: peer dependency missing");
    }) as unknown as DshBootDeps["boot"];
    const exit = vi.spyOn(process, "exit").mockImplementation((() => undefined) as never);

    await bootDsh({
      boot,
      env: { [ENV_DOMINION_ARTIFACT_DIR]: "/dominion/game/agent-v2" },
      secretDir: "/tmp/secret",
      readSecretFile: () => TOKEN,
    });

    expect(boot).toHaveBeenCalledTimes(1);
    expect(consoleError).toHaveBeenCalledWith(expect.stringContaining("boot failed (fail-loud)"));
    expect(consoleError).toHaveBeenCalledWith(expect.stringContaining("peer dependency missing"));
    expect(exit).toHaveBeenCalledWith(1);
  });

  it("passes the boot call shape (bin name, cordis.yml path, bare-module anchor)", async () => {
    const ctx = { marker: "ctx" } as unknown as DshContext;
    const boot = fakeBoot(ctx);
    const exit = vi.spyOn(process, "exit").mockImplementation((() => undefined) as never);

    await bootDsh({
      boot,
      env: { [ENV_DOMINION_ARTIFACT_DIR]: "/dominion/game/agent-v2" },
      secretDir: "/tmp/secret",
      readSecretFile: () => TOKEN,
    });

    expect(boot).toHaveBeenCalledTimes(1);
    const args = boot.mock.calls[0] as unknown[];
    expect(args[0]).toBe("game-agent-v2");
    expect(args[1]).toBe(cordisConfigPath());
    expect(args[2]).toBeUndefined();
    expect(args[3]).toBeUndefined();
    // The bare-module anchor pins plugin resolution at this module.
    expect(String(args[4])).toContain("dsh.ts");
  });
});

// ── composition manifest (specs/070-dsh-upgrade/contracts/composition-manifest.md §2) ──

/** One parsed cordis.yml entry row (`!!js` scalars round-trip as their text). */
interface ManifestRow {
  id: string;
  name: string;
  config?: Record<string, unknown>;
}

/**
 * Parse the composition manifest with the Loader's entry-list dialect: the
 * `!!js` scalars are expression nodes the Loader evaluates at activation
 * (dsh-app-boot JsExpr tag); for assertions the expression text itself is
 * the expected value, so the tag resolves to its verbatim source.
 */
function loadManifest(): ManifestRow[] {
  const jsExpr = defineScalarTag("tag:yaml.org,2002:js", {
    resolve: (source: string) => source,
  });
  const schema = YAML11_SCHEMA.withTags(jsExpr);
  const text = fs.readFileSync(cordisConfigPath(), "utf8");
  return load(text, { schema }) as ManifestRow[];
}

/**
 * One sub-plugin entry of a pool template's `plugins` list; a persona entry
 * carries its `prefix` (the `@deepseek-ai/dsh-persona` Config's prefix field).
 */
interface PresetPluginRow {
  name: string;
  config?: Record<string, unknown>;
}

/**
 * One pool template's definition, injected through the preset-authoring row
 * config (`templates`, specs/070-dsh-upgrade/contracts/preset-model-mapping.md
 * §3): the snapshot is the SINGLE source of truth for the player/planner
 * templates, and the authoring plugin derives store records from it without
 * reading any template filesystem
 * (specs/070-dsh-upgrade/research.md D4 终态结论).
 */
interface TemplateDeclaration {
  name: string;
  description: string;
  plugins: PresetPluginRow[];
}

/**
 * Player pool persona prefix. The fake-llm team fixture's `system_keywords`
 * match the opening `你是扫雷 player` by containment, so the identity anchor
 * MUST stay verbatim (specs/070-dsh-upgrade/contracts/composition-manifest.md
 * §2; projects/game/fake-llm/service/testdata/team_{player,planner}.yaml).
 */
const PLAYER_PERSONA_PREFIX =
  "你是扫雷 player，负责操作桌面扫雷窗口完成对局，并依据 saolei 工具返回的棋盘文本持续推理落子，直到对局结束。你独占桌面控制；一律以工具返回的棋盘事实为准，任何与棋盘不符的说法都先核对再行动；每局结束后是否开启新局由你自行判断。保持冷静、精确，落子前先核对已揭示信息与剩余雷数，不做无依据的猜测。";

/** Planner pool persona prefix — the verbatim identity anchor for planner. */
const PLANNER_PERSONA_PREFIX =
  "你是扫雷 planner，负责对局策略与复盘：依据对局记录复盘得失，把值得跨局保留的观察写入长期记忆（调用 memory 工具），并给出下一局可执行的策略。你不操作桌面、不调用扫雷操作工具；策略应具体、可执行，并与长期记忆中的历史观察保持一致；复盘时区分偶然结果与可复用经验，避免把单局噪声当作长期结论。";

/**
 * The declared player template: the persona row plus the plug-and-play
 * self-contained plugin list (persona-only in the current composition).
 * Capability wiring (saolei) rides the saolei-loop capability-dependency list,
 * not the template
 * (specs/074-dsh-grpc-runtime/contracts/preset-templates.md §1/§3).
 */
const PLAYER_TEMPLATE: TemplateDeclaration = {
  name: "player",
  description:
    "扫雷 player 池模板：persona 行与即插即用自包含插件列表；saolei 能力经 saolei-loop 依赖列表组装",
  plugins: [{ name: "@deepseek-ai/dsh-persona", config: { prefix: PLAYER_PERSONA_PREFIX } }],
};

/** The declared planner template: persona-only; memory rides the saolei-loop list. */
const PLANNER_TEMPLATE: TemplateDeclaration = {
  name: "planner",
  description:
    "扫雷 planner 池模板：persona 行与即插即用自包含插件列表；memory 能力经 saolei-loop 依赖列表组装",
  plugins: [{ name: "@deepseek-ai/dsh-persona", config: { prefix: PLANNER_PERSONA_PREFIX } }],
};

/**
 * One flat capability-dependency row on the saolei-loop row config
 * (specs/074-dsh-grpc-runtime/contracts/team-assembly.md §2): no role group,
 * the memory adapter carrying the scenario connection config.
 */
interface CapabilityPluginRow {
  name: string;
  config?: Record<string, unknown>;
}

const SAOLEI_LOOP_PLUGINS: CapabilityPluginRow[] = [
  { name: "@dominion/dsh-saolei-tools" },
  {
    name: "@dominion/dsh-saolei-memory",
    config: {
      target: "dominion:///game/memory:50051",
      protoPath: "projects/game/game.proto",
    },
  },
];

describe("cordis.yml composition manifest", () => {
  const rows = loadManifest();
  /**
   * The pool-template definitions: the `templates` snapshot in the
   * preset-authoring row config is the single source of truth for the
   * player/planner templates
   * (specs/070-dsh-upgrade/contracts/preset-model-mapping.md §3;
   * specs/070-dsh-upgrade/research.md D4 终态结论).
   */
  const templates = rows.find((row) => row.id === "preset-authoring")?.config
    ?.templates as Record<string, TemplateDeclaration> | undefined;

  it("keeps preset selection fail-loud: the registry default points at no preset", () => {
    const registry = rows.find((row) => row.id === "agent-preset-registry");
    expect(registry?.name).toBe("@deepseek-ai/dsh-agent-preset-registry");
    // Preset selection is mandatory in this service. The empty id is accepted
    // by the registry Config — `z.string()` accepts "", the "defined"
    // requirement only rejects an absent value (the registry package's 0.2.0
    // diff touches only its typert protocol snapshot, so this Config surface
    // holds; specs/073-dsh-0.2-upgrade/research.md D2) — and no roster entry
    // claims it,
    // so an id-less resolve matches no definition and reports
    // agent-preset/not-found instead of silently falling back to a template.
    expect(registry?.config).toEqual({ default: "" });
    // Templates are not declaration rows: the manifest carries ZERO
    // @deepseek-ai/dsh-agent-preset rows, so the roster only ever holds the
    // presets preset-authoring materializes from store records — and its id
    // grammar (/^[a-z0-9][a-z0-9-]*$/) rejects the empty string, so the empty
    // default matches nothing
    // (specs/070-dsh-upgrade/contracts/composition-manifest.md §2;
    // specs/070-dsh-upgrade/research.md D4 终态结论).
    expect(rows.filter((row) => row.name === "@deepseek-ai/dsh-agent-preset")).toEqual([]);
  });

  it("injects the two pool templates as the single-source authoring snapshot", () => {
    expect(Object.keys(templates ?? {})).toEqual(["player", "planner"]);
    // The authoring row config carries the template definitions verbatim and
    // is their single source of truth — no declaration row repeats them
    // (specs/070-dsh-upgrade/contracts/preset-model-mapping.md §3;
    // specs/070-dsh-upgrade/research.md D4 终态结论).
    expect(templates?.player).toEqual(PLAYER_TEMPLATE);
    expect(templates?.planner).toEqual(PLANNER_TEMPLATE);
  });

  it("keeps the cross-phase persona identity anchors in the template snapshot prefixes", () => {
    const playerPrefix = templates?.player.plugins[0]?.config?.prefix as string;
    // The fake-llm team fixture matches this opening by containment, so the
    // anchor must stay verbatim
    // (projects/game/fake-llm/service/testdata/team_player.yaml).
    expect(playerPrefix.startsWith("你是扫雷 player")).toBe(true);
    expect(playerPrefix).toContain("saolei");
    expect(playerPrefix).not.toContain("memory");
    // R2 boundary: no team-level facts (broadcast wrappers/roster).
    expect(playerPrefix).not.toContain("-message>");
    expect(playerPrefix).not.toContain("名册");

    const plannerPrefix = templates?.planner.plugins[0]?.config?.prefix as string;
    expect(plannerPrefix.startsWith("你是扫雷 planner")).toBe(true);
    expect(plannerPrefix).toContain("复盘");
    expect(plannerPrefix).toContain("memory");
    expect(plannerPrefix).not.toContain("-message>");
    expect(plannerPrefix).not.toContain("名册");
  });

  it("keeps the pool templates persona-only (capability wiring rides the saolei-loop list)", () => {
    // Plug-and-play list = self-contained plugins only; the persona row is the
    // current content. Tool capabilities (saolei/memory) need team-layer
    // assembly and are declared on the saolei-loop row's config.plugins instead
    // (specs/074-dsh-grpc-runtime/contracts/preset-templates.md §1/§2/§5).
    const playerRows = templates?.player.plugins.map((plugin) => plugin.name) ?? [];
    expect(playerRows).toEqual(["@deepseek-ai/dsh-persona"]);

    const plannerRows = templates?.planner.plugins.map((plugin) => plugin.name) ?? [];
    expect(plannerRows).toEqual(["@deepseek-ai/dsh-persona"]);
  });

  it("direct-composes the 22-row plugin set in contract order", () => {
    expect(rows.map((row) => row.id)).toEqual([
      "timer",
      "llm",
      "session",
      "session-projection",
      "system-prompt",
      "tools",
      "agents",
      "agent-loop",
      "invariants",
      "invariant-session",
      "invariant-agent",
      "invariant-scope",
      "llm-retry",
      "agent-preset-registry",
      "grpc-runtime",
      "saolei",
      "llm-glm",
      "llm-opencode-go",
      "desktop-bridge",
      "preset-authoring",
      "team",
      "saolei-loop",
    ]);
    // The row names are pinned alongside the ids so the set is absolutely
    // explicit (no runtime baseline, no subset matching). The pool templates
    // are carried by the preset-authoring row config, not as rows
    // (specs/070-dsh-upgrade/contracts/composition-manifest.md §2;
    // specs/070-dsh-upgrade/research.md D4 终态结论).
    expect(rows.map((row) => row.name)).toEqual([
      "@deepseek-ai/cordis-plugin-timer",
      "@deepseek-ai/dsh-llm",
      "@deepseek-ai/dsh-session",
      "@deepseek-ai/dsh-session-projection",
      "@deepseek-ai/dsh-system-prompt",
      "@deepseek-ai/dsh-tools",
      "@deepseek-ai/dsh-agent",
      "@deepseek-ai/dsh-agent-loop",
      "@deepseek-ai/dsh-invariants",
      "@deepseek-ai/dsh-session/invariant",
      "@deepseek-ai/dsh-agent/invariant",
      "@deepseek-ai/dsh-scope/invariant",
      "@deepseek-ai/dsh-llm-retry",
      "@deepseek-ai/dsh-agent-preset-registry",
      "@dominion/dsh-grpc-runtime",
      "@dominion/dsh-saolei",
      "@dominion/dsh-llm-glm",
      "@dominion/dsh-llm-opencode-go",
      "@dominion/dsh-desktop-bridge",
      "@dominion/dsh-preset-authoring",
      "@dominion/dsh-team",
      "@dominion/dsh-saolei-loop",
    ]);
  });

  it("mounts the official agent loop and no spine row (R1/R7)", () => {
    const names = rows.map((row) => row.name);
    // 官方驱动回归：the agent-loop row owns the factory (Config.agents[]
    // left empty — agents materialize dynamically through ctx.agents.create).
    expect(names).toContain("@deepseek-ai/dsh-agent-loop");
    // The loop and the preset registry both inject `sessionProjections`, so
    // the registry service row must be composed (specs/070-dsh-upgrade/
    // research.md D6).
    expect(names).toContain("@deepseek-ai/dsh-session-projection");
    expect(names).not.toContain("@deepseek-ai/dsh-agent-spine-demo");
    expect(names.filter((name) => name.includes("spine"))).toEqual([]);
  });

  it("trims the system-prompt config (no harness identity, no runtime context)", () => {
    const row = rows.find((entry) => entry.id === "system-prompt");
    expect(row?.config).toEqual({
      includeHarnessIdentity: false,
      includeRuntimeContext: false,
    });
  });

  it("mounts the invariant companions as subpath rows (specs/051-agent-v2-dsh-migration/research.md D5 case A)", () => {
    expect(rows.find((row) => row.id === "invariant-session")?.name).toBe(
      "@deepseek-ai/dsh-session/invariant",
    );
    expect(rows.find((row) => row.id === "invariant-agent")?.name).toBe(
      "@deepseek-ai/dsh-agent/invariant",
    );
    expect(rows.find((row) => row.id === "invariant-scope")?.name).toBe(
      "@deepseek-ai/dsh-scope/invariant",
    );
  });

  it("keeps the GLM adapter row with the env-sourced dual-model catalog", () => {
    const row = rows.find((entry) => entry.id === "llm-glm");
    expect(row?.name).toBe("@dominion/dsh-llm-glm");
    const config = row?.config as {
      apiKeyEnv: string;
      baseURL: string;
      models: Array<{ id: string; contextWindow: number }>;
    };
    expect(config.apiKeyEnv).toBe("GLM_API_KEY");
    expect(config.baseURL).toContain("GLM_BASE_URL");
    // The catalog leads with the SAME env-parameterized expression the host's
    // default-model resolution uses (specs/054-agent-v2-bugfixes/
    // contracts/agent-api-changes.md §5 同源), followed by the literal flash
    // variant (specs/054-agent-v2-bugfixes/data-model.md §4).
    expect(config.models).toHaveLength(2);
    expect(config.models[0].id).toContain("GLM_MODEL");
    expect(config.models[0].id).toContain("glm-5.3");
    expect(config.models[0].contextWindow).toBe(1_000_000);
    expect(config.models[1]).toEqual({ id: "glm-5.3-flash", contextWindow: 1_000_000 });
  });

  it("keeps the opencode-go adapter row with the full 16-model Chat Completions catalog", () => {
    const row = rows.find((entry) => entry.id === "llm-opencode-go");
    expect(row?.name).toBe("@dominion/dsh-llm-opencode-go");
    const config = row?.config as {
      apiKeyEnv: string;
      baseURL: string;
      models: Array<{ id: string; contextWindow: number }>;
      streamIdleTimeoutMs: string;
    };
    expect(config.apiKeyEnv).toBe("OPENCODE_API_KEY");
    expect(config.baseURL).toContain("OPENCODE_BASE_URL");
    // The watchdog window env is wrapped in Number(): Config validates a
    // number while env values are strings
    // (specs/063-llm-reliability-opencode-go/research.md D13).
    expect(config.streamIdleTimeoutMs).toContain(
      "Number(process.env.OPENCODE_STREAM_IDLE_TIMEOUT_MS)",
    );
    expect(config.streamIdleTimeoutMs).toContain("300000");
    // The full FR-015 catalog must be injected: a partial `models` list would
    // replace the plugin's default directory wholesale
    // (specs/063-llm-reliability-opencode-go/contracts/opencode-go-plugin.md §2).
    expect(config.models).toHaveLength(16);
    expect(config.models[0].id).toContain("OPENCODE_MODEL");
    expect(config.models[0].id).toContain("glm-5.3");
    expect(config.models[0].contextWindow).toBe(1_000_000);
    expect(config.models.slice(1)).toEqual([
      { id: "glm-5.3-flash", contextWindow: 1_000_000 },
      { id: "glm-5.2", contextWindow: 1_000_000 },
      { id: "glm-5.1", contextWindow: 202_752 },
      { id: "kimi-k3", contextWindow: 1_048_576 },
      { id: "kimi-k2.7-code", contextWindow: 262_144 },
      { id: "kimi-k2.6", contextWindow: 262_144 },
      { id: "longcat-2.0", contextWindow: 1_000_000 },
      { id: "deepseek-v4.1-flash", contextWindow: 1_000_000 },
      { id: "deepseek-v4-pro", contextWindow: 1_000_000 },
      { id: "deepseek-v4-flash", contextWindow: 1_000_000 },
      { id: "deepseek-v4-flash-vision-exp", contextWindow: 1_000_000 },
      { id: "mimo-v2.5", contextWindow: 1_000_000 },
      { id: "mimo-v2.5-pro", contextWindow: 1_048_576 },
      { id: "hy4-preview", contextWindow: 1_024_000 },
      { id: "hy3", contextWindow: 256_000 },
    ]);
  });

  it("mounts the authoring plugin with the host-injected Mongo connection and the template snapshot", () => {
    const row = rows.find((entry) => entry.id === "preset-authoring");
    expect(row?.name).toBe("@dominion/dsh-preset-authoring");
    // The credential/URI resolution is the host's (presets.ts, T006) —
    // injected via the MONGO_URI environment variable, the GLM_BASE_URL
    // injection pattern; the plugin carries no Dominion deployment logic.
    expect(row?.config).toEqual({
      storage: "mongo",
      mongoUri: expect.stringContaining("MONGO_URI"),
      mongoDatabase: "game_agent_v2",
      mongoCollection: "presets",
      // The template snapshot is the single source of truth for the pool
      // templates (specs/074-dsh-grpc-runtime/contracts/preset-templates.md
      // §1): the authoring plugin derives store records into registry
      // declarations from this in-manifest data, reading no template
      // filesystem. Its `plugins` list is the persona-only plug-and-play list.
      templates: {
        player: PLAYER_TEMPLATE,
        planner: PLANNER_TEMPLATE,
      },
    });
    // The exact toEqual above also pins the REMOVED template-row rule key: the
    // authoring config carries no rejection-path table, so preset creation has
    // no template-row validation
    // (specs/074-dsh-grpc-runtime/contracts/preset-templates.md §3).
  });

  it("mounts the team row and the team-loop row with the flat capability-dependency list", () => {
    const byId = new Map(rows.map((row) => [row.id, row.name]));
    expect(byId.get("desktop-bridge")).toBe("@dominion/dsh-desktop-bridge");
    expect(byId.get("grpc-runtime")).toBe("@dominion/dsh-grpc-runtime");
    expect(byId.get("team")).toBe("@dominion/dsh-team");
    expect(byId.get("memory")).toBeUndefined();
    expect(byId.get("saolei")).toBe("@dominion/dsh-saolei");
    expect(byId.get("saolei-loop")).toBe("@dominion/dsh-saolei-loop");
    // The capability dependencies are declared FLAT on the saolei-loop row:
    // no role grouping, the memory adapter carrying the connection config
    // (specs/074-dsh-grpc-runtime/contracts/team-assembly.md §2;
    // specs/070-dsh-upgrade/contracts/composition-manifest.md §2).
    const saoleiLoop = rows.find((row) => row.id === "saolei-loop");
    expect(saoleiLoop?.config).toEqual({ plugins: SAOLEI_LOOP_PLUGINS });
  });

  it("mounts the top-level saolei foundation row without config", () => {
    // The game foundation is a composition-level service row (the top-level
    // `saolei` id), not a saolei-loop config.plugins sub-plugin; the tools
    // capability rides the saolei-loop list instead
    // (specs/075-saolei-game-foundation/contracts/cutover-contracts.md §2;
    // contracts/saolei-foundation.md §1).
    const saolei = rows.find((row) => row.id === "saolei");
    expect(saolei?.name).toBe("@dominion/dsh-saolei");
    expect(saolei?.config).toBeUndefined();
  });

  it("composes no session-persistence and no settings row (install-level peers only)", () => {
    // The exact 22-row set is pinned by the order test above; this makes the
    // declaration-level distinction explicit: settings and
    // session-persistence are install-level peers (package.json + BUILD
    // closure) without a composed row
    // (specs/070-dsh-upgrade/contracts/composition-manifest.md §4).
    const names = rows.map((row) => row.name);
    expect(names).not.toContain("@deepseek-ai/dsh-session-persistence");
    expect(names).not.toContain("@deepseek-ai/dsh-settings");
  });
});
