import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import * as http from "node:http";
import * as fs from "node:fs";
import * as path from "node:path";
import type { AddressInfo } from "node:net";
import { boot } from "@deepseek-ai/dsh-app-boot";
import { scopeOf, scopeParentOf } from "@deepseek-ai/dsh-scope";
import { cordisConfigPath } from "./dsh.js";
import { AgentSessions } from "./session.js";
import type { DshContext } from "./dsh.js";

/**
 * Integration tests over the REAL direct composition: the shipped cordis.yml
 * boots in-process (same manifest the deployed artifact carries), with the
 * roster roots pointed at the runfiles template presets and the LLM adapter
 * pointed at a local capture server. This is the single-test surface of the
 * verification matrix (specs/058-dsh-preset-roster-demo/research.md R11,
 * re-based on the 060 derivation semantics —
 * specs/060-agent-v2-team-optimize/contracts/preset-derivation.md §2):
 *
 *   - V1-1  per-session composition difference (persona / system prompt)
 *           plus the deployment-persona shadowing, asserted on the llm
 *           request surface (what the model actually receives)
 *   - V1-3  the resolved preset id lands in the session header, and an
 *           id-less create is rejected INVALID_ARGUMENT (preset selection is
 *           mandatory — the roster default is gone)
 *   - V2-1  the preset-row tool is visible only to preset members
 *           (global layer empty, member-scope view populated)
 *   - V2-2  two sessions on one store preset each hold their OWN derived
 *           mount (no shared standing registration) while presenting the
 *           same persona and tool catalog
 *
 * The sessions bind STORE presets authored below through the real
 * `ctx.presetAuthoring.create`; template ids are derivation sources, no
 * longer directly composable. The only seams are environmental (roots/baseURL
 * env vars read by the cordis.yml `!!js` expressions) — the composition
 * itself is unmocked. The mock LLM lives below the llm adapter boundary,
 * mirroring the fake-llm wire contract
 * (specs/047-dsh-chat-demo/contracts/fake-llm-wire.md §3).
 */

/** The deployment persona from cordis.yml — must be shadowed by presets (R12). */
const DEPLOYMENT_PERSONA = "You are a helpful demo chat assistant.";

/** The reply text the mock LLM streams for every request. */
const MOCK_REPLY = "composition-mock-reply";

/**
 * The store presets the sessions bind, authored once in beforeAll through the
 * real authoring service (060 preset derivation: only store records compose;
 * template ids are derivation sources). An empty persona keeps each template's
 * persona row, so the model-visible compositions match the template presets
 * exactly.
 */
const TOOLS_PRESET = "comp-tools-preset";
const STANDARD_PRESET = "comp-standard-preset";

/** One captured chat-completions request body. */
interface CapturedRequest {
  systemText: string;
  userText: string;
  toolNames: string[];
}

/**
 * The composition surface this test reads, typed structurally so the file
 * compiles standalone (the full Context surface comes from the dsh packages'
 * declaration merges, which this test-only file does not need in full).
 * The brands are erased at runtime, so plain ids/objects satisfy the real
 * services.
 */
type ScopeContext = Parameters<typeof scopeOf>[0];

interface CompositionSurface {
  presetAuthoring: {
    create(input: { id: string; template: string; persona: string }): Promise<{ id: string }>;
  };
  agents: {
    get(id: string): {
      ctx: ScopeContext;
      session: { header: { agentPreset?: string } };
    } | undefined;
  };
  tools: {
    get(name: string, scope?: Parameters<typeof scopeParentOf>[0]): { name: string } | undefined;
  };
}

/** Minimal SSE chat-completions server that captures every request body. */
async function startMockLlm(): Promise<{
  server: http.Server;
  url: string;
  requests: CapturedRequest[];
}> {
  const requests: CapturedRequest[] = [];
  const server = http.createServer((req, res) => {
    if (req.method !== "POST" || !req.url?.endsWith("/chat/completions")) {
      res.writeHead(404).end();
      return;
    }
    const chunks: Buffer[] = [];
    req.on("data", (chunk: Buffer) => chunks.push(chunk));
    req.on("end", () => {
      const body = JSON.parse(Buffer.concat(chunks).toString("utf8")) as {
        messages?: Array<{ role: string; content: string }>;
        tools?: Array<{ function?: { name?: string } }>;
      };
      requests.push({
        systemText: (body.messages ?? [])
          .filter((m) => m.role === "system")
          .map((m) => m.content)
          .join("\n"),
        userText:
          [...(body.messages ?? [])]
            .reverse()
            .find((m) => m.role === "user")
            ?.content ?? "",
        toolNames: (body.tools ?? []).map((t) => t.function?.name ?? ""),
      });

      const frame = (delta: Record<string, unknown>, extra: Record<string, unknown> = {}) =>
        `data: ${JSON.stringify({
          id: "chatcmpl-composition-test",
          object: "chat.completion.chunk",
          created: 0,
          model: "fake-chat-v1",
          choices: [{ index: 0, delta, finish_reason: null, ...extra }],
        })}\n\n`;
      const finishFrame = `data: ${JSON.stringify({
        id: "chatcmpl-composition-test",
        object: "chat.completion.chunk",
        created: 0,
        model: "fake-chat-v1",
        choices: [{ index: 0, delta: {}, finish_reason: "stop" }],
        usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2 },
      })}\n\n`;
      res.writeHead(200, { "Content-Type": "text/event-stream" });
      res.end(
        [
          frame({ role: "assistant", content: "" }),
          frame({ content: MOCK_REPLY }),
          finishFrame,
          "data: [DONE]\n\n",
        ].join(""),
      );
    });
  });
  server.listen(0, "127.0.0.1");
  const address = await new Promise<AddressInfo>((resolve, reject) => {
    server.once("listening", () => resolve(server.address() as AddressInfo));
    server.once("error", reject);
  });
  return { server, url: `http://127.0.0.1:${address.port}/v1`, requests };
}

/**
 * The bazel runfiles materialize node_modules as per-package proportional
 * links, which drops the pnpm hidden hoist layer that `@deepseek-ai/
 * cordis-plugin-loader` relies on at runtime: its internal-ESM-loader
 * helper (@deepseek-ai/node-addon-require-builtin) requires its platform
 * binding BY NAME from @deepseek-ai/node-addon-native-custom-loader's
 * location — a dynamic optional dependency no static linker can foresee.
 * Repair the two resolution spots in the runfiles store (idempotent, the
 * links already exist under a real pnpm install) so the loader can acquire
 * Node's internal loader and resolve row specifiers from the boot anchor.
 * Without this the loader falls back to resolving rows from its own store
 * location, where the composition rows are absent.
 */
function repairRunfilesNativeBindingLinks(): void {
  let dir = path.resolve(import.meta.dirname);
  let store = "";
  for (let i = 0; i < 10 && !store; i++) {
    const candidate = path.join(dir, "node_modules", ".aspect_rules_js");
    if (fs.existsSync(candidate)) store = candidate;
    dir = path.dirname(dir);
  }
  if (!store) return; // Not a runfiles layout (plain pnpm tree resolves naturally).
  const bindingEntry = fs
    .readdirSync(store)
    .find((e) => e.startsWith("node-addon-require-builtin-linux-x64-gnu@"));
  if (!bindingEntry) return;
  const bindingTarget = path.join(
    store,
    bindingEntry,
    "node_modules",
    "node-addon-require-builtin-linux-x64-gnu",
  );
  const spots = [
    path.join(
      store,
      "node-addon-native-custom-loader@0.1.5",
      "node_modules",
      "node-addon-require-builtin-linux-x64-gnu",
    ),
    path.join(store, "node-addon-native-custom-loader@0.1.5", "node_modules", "linux-x64-gnu"),
  ];
  for (const spot of spots) {
    if (fs.existsSync(spot)) continue;
    try {
      fs.symlinkSync(bindingTarget, spot, "dir");
    } catch {
      // Best-effort: a racing or read-only layout surfaces later as the
      // loader's own "Cannot find package" diagnostics.
    }
  }
}

describe("composition (real direct-compose boot)", () => {
  let ctx: DshContext;
  /** The composition services this test reads (see {@link CompositionSurface}). */
  let surface: CompositionSurface;
  let sessions: AgentSessions;
  let mock: Awaited<ReturnType<typeof startMockLlm>>;

  beforeAll(async () => {
    mock = await startMockLlm();
    repairRunfilesNativeBindingLinks();
    // The roster scans the runfiles copy of the deployed template presets
    // (same files the artifact ships); the sessions below bind store presets
    // authored after boot — no writable root exists under the 060 derivation
    // semantics.
    process.env.FAKE_LLM_API_KEY = "dummy-key";
    process.env.FAKE_LLM_BASE_URL = mock.url;
    process.env.PRESET_TEMPLATES_ROOT = path.resolve(
      import.meta.dirname,
      "..",
      "presets-templates",
    );
    try {
      ctx = (await boot(
        "dsh-demo-agent",
        cordisConfigPath(),
        undefined,
        undefined,
        import.meta.url,
      )) as DshContext;
    } catch (err) {
      // The loader wraps row failures in AggregateErrors; surface them or
      // the test reports only the outer wrapper (fail-loud diagnostics).
      const seen: unknown[] = [];
      const walk = (e: unknown): void => {
        // AggregateErrors carry the row failures in `.errors`; wrappers
        // chain them through `.cause`. Print every layer, otherwise the
        // test reports only the outer wrapper.
        const aggregated = e as { errors?: unknown[] };
        if (Array.isArray(aggregated?.errors)) {
          for (const inner of aggregated.errors) {
            if (!seen.includes(inner)) {
              seen.push(inner);
              walk(inner);
            }
          }
        }
        const cause = (e as { cause?: unknown })?.cause;
        if (cause !== undefined && !seen.includes(cause)) {
          seen.push(cause);
          walk(cause);
        }
        if (Array.isArray(aggregated?.errors) || cause !== undefined) {
          console.error("boot failure layer:", e instanceof Error ? e.message : e);
          return;
        }
        console.error("boot row failure:", e);
      };
      walk(err);
      throw err;
    }
    surface = ctx as unknown as CompositionSurface;
    sessions = new AgentSessions(ctx);
    // Author the store presets the sessions bind: only store records compose
    // under the 060 derivation semantics. An empty persona keeps each
    // template's persona row (the role default base).
    await surface.presetAuthoring.create({ id: TOOLS_PRESET, template: "demo-tools", persona: "" });
    await surface.presetAuthoring.create({ id: STANDARD_PRESET, template: "demo-standard", persona: "" });
    // Timeouts stay well inside the bazel small-size 60s wall limit, where
    // they can actually fire; the observed boot is sub-second.
  }, 30_000);

  afterAll(async () => {
    // Null-guarded: a beforeAll boot failure must surface its own error,
    // not mask it with a TypeError from the cleanup path.
    if (sessions) await sessions.shutdown();
    mock?.server.close();
    delete process.env.FAKE_LLM_API_KEY;
    delete process.env.FAKE_LLM_BASE_URL;
    delete process.env.PRESET_TEMPLATES_ROOT;
  }, 15_000);

  it(
    "V1-1: two preset sessions present different personas and tool catalogs, " +
      "and the deployment persona is shadowed",
    async () => {
      await sessions.create("comp-conv-tools", TOOLS_PRESET);
      await sessions.create("comp-conv-standard", STANDARD_PRESET);

      const toolsRound = sessions.send("comp-conv-tools", "hello");
      await vi.waitFor(() => expect(mock.requests.length).toBeGreaterThanOrEqual(1));
      await expect(toolsRound).resolves.toBe(MOCK_REPLY);
      const toolsRequest = mock.requests.at(-1)!;

      const standardRound = sessions.send("comp-conv-standard", "hello");
      await vi.waitFor(() => expect(mock.requests.length).toBeGreaterThanOrEqual(2));
      await expect(standardRound).resolves.toBe(MOCK_REPLY);
      const standardRequest = mock.requests.at(-1)!;

      // Tools session: its persona, its tool schema, and its guidance.
      expect(toolsRequest.systemText).toContain("You are the demo tools assistant.");
      expect(toolsRequest.toolNames).toContain("demo_echo");
      expect(toolsRequest.systemText).toContain("demo_echo");

      // Standard session: the other persona, neither the tool nor its guidance.
      expect(standardRequest.systemText).toContain("You are the demo standard assistant.");
      expect(standardRequest.toolNames).not.toContain("demo_echo");
      expect(standardRequest.systemText).not.toContain("demo_echo");

      // Neither session sees the deployment persona — the preset persona row
      // shadows it (nearest scope wins over the global layer).
      expect(toolsRequest.systemText).not.toContain(DEPLOYMENT_PERSONA);
      expect(standardRequest.systemText).not.toContain(DEPLOYMENT_PERSONA);
    },
    30_000,
  );

  it("V1-3: the session header records the resolved preset; an id-less create is rejected", async () => {
    await sessions.create("comp-conv-explicit", TOOLS_PRESET);

    // ctx.agents.get returns the bare live agent; the header is the durable
    // creation metadata the factory folded from meta.
    expect(surface.agents.get("comp-conv-explicit")?.session.header.agentPreset).toBe(TOOLS_PRESET);

    // Preset selection is mandatory under the 060 derivation semantics (the
    // roster default is gone): an id-less create reaches compose(undefined)
    // and is rejected INVALID_ARGUMENT before any agent is published.
    await expect(sessions.create("comp-conv-default")).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
    });
    expect(surface.agents.get("comp-conv-default")).toBeUndefined();
  }, 30_000);

  it("V2-1: the preset-row tool is visible only to preset members", async () => {
    // Self-contained conversations: this test must not depend on the
    // sessions V1-1 created, so a V1-1 failure does not cascade here.
    await sessions.create("comp-conv-member-tools", TOOLS_PRESET);
    await sessions.create("comp-conv-member-standard", STANDARD_PRESET);
    const toolsAgent = surface.agents.get("comp-conv-member-tools");
    const standardAgent = surface.agents.get("comp-conv-member-standard");
    expect(toolsAgent).toBeDefined();
    expect(standardAgent).toBeDefined();

    // Global layer (no scope): the host composition registers no model-facing
    // tools, so demo_echo is absent.
    expect(surface.tools.get("demo_echo")).toBeUndefined();
    // A member of the demo-tools mount resolves it; a member of the sibling
    // standard mount does not (sibling preset layers stay deaf).
    expect(surface.tools.get("demo_echo", scopeOf(toolsAgent!.ctx))).toBeDefined();
    expect(surface.tools.get("demo_echo", scopeOf(standardAgent!.ctx))).toBeUndefined();
  }, 30_000);

  it("V2-2: two sessions on one store preset each hold their own derived mount", async () => {
    await sessions.create("comp-conv-shared-a", TOOLS_PRESET);
    await sessions.create("comp-conv-shared-b", TOOLS_PRESET);
    const agentA = surface.agents.get("comp-conv-shared-a");
    const agentB = surface.agents.get("comp-conv-shared-b");
    expect(agentA).toBeDefined();
    expect(agentB).toBeDefined();

    const scopeA = scopeOf(agentA!.ctx);
    const scopeB = scopeOf(agentB!.ctx);
    expect(scopeA).toBeDefined();
    expect(scopeB).toBeDefined();

    // Per-agent derived mounts: each session's composition is a mountPreset
    // subtree owned by that member's scope, so both sessions resolve
    // demo_echo through DISTINCT registrations — there is no shared standing
    // mount anymore.
    const viewA = surface.tools.get("demo_echo", scopeA!);
    const viewB = surface.tools.get("demo_echo", scopeB!);
    expect(viewA).toBeDefined();
    expect(viewB).toBeDefined();
    expect(viewB).not.toBe(viewA);

    // Content equivalence: the same store preset derives the same persona
    // and tool catalog for both sessions.
    const before = mock.requests.length;
    const roundA = sessions.send("comp-conv-shared-a", "hello");
    await vi.waitFor(() => expect(mock.requests.length).toBeGreaterThan(before));
    await expect(roundA).resolves.toBe(MOCK_REPLY);
    const requestA = mock.requests.at(-1)!;
    const afterA = mock.requests.length;
    const roundB = sessions.send("comp-conv-shared-b", "hello");
    await vi.waitFor(() => expect(mock.requests.length).toBeGreaterThan(afterA));
    await expect(roundB).resolves.toBe(MOCK_REPLY);
    const requestB = mock.requests.at(-1)!;

    expect(requestA.systemText).toContain("You are the demo tools assistant.");
    expect(requestB.systemText).toContain("You are the demo tools assistant.");
    expect(requestB.toolNames).toEqual(requestA.toolNames);
    expect(requestA.toolNames).toContain("demo_echo");
  }, 30_000);
});
