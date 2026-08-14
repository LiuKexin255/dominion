/**
 * skill-loader.ts — Built-in skill body loader + mcp_name → skill registry.
 *
 * Loads the body (Markdown, WITHOUT frontmatter) of built-in skills authored
 * under `src/skill/{name}/SKILL.md`, and maps `mcp_names` profile entries
 * to their matching built-in skills (spec 018-saolei-mcp FR-024,
 * research.md D9 — "reads `src/skill/{name}/SKILL.md` files (bundled into
 * the agent image)" and "append the SKILL.md body to the systemPrompt").
 *
 * The SKILL.md files are carried into the deployed image via the
 * `artifact_pkg_js` `data_files` attribute; at runtime they are resolved
 * relative to this module's `__dirname` (the project compiles to CommonJS,
 * tsconfig.json `"module": "commonjs"`).
 */

import { readFileSync } from "node:fs";
import { join } from "node:path";

/**
 * Separator inserted between the original systemPrompt and the appended
 * skill body when injecting a built-in skill (research.md D9).
 */
export const SKILL_PROMPT_SEPARATOR = "\n\n---\n\n";

/**
 * Read the body of a built-in skill Markdown file, stripping its YAML
 * frontmatter.
 *
 * The file is resolved at `{__dirname}/skill/{name}/SKILL.md`. In the
 * deployed image the `.md` is placed alongside the compiled JS (via
 * `artifact_pkg_js` `data_files`), so the path resolves under the service
 * directory. In tests the `.md` is in the vitest_test runfiles.
 *
 * @param name The skill folder name (e.g. `"saolei"`).
 * @returns The skill body (Markdown without frontmatter), or `""` when the
 *   file is missing (no crash — same behavior as an unknown skill).
 */
function readSkillBody(name: string): string {
	const mdPath = join(__dirname, "skill", name, "SKILL.md");
	let raw: string;
	try {
		raw = readFileSync(mdPath, "utf8");
	} catch {
		return "";
	}
	const m = raw.match(/^---\n[\s\S]*?\n---\n([\s\S]*)$/);
	if (!m) {
		return raw.replace(/^\n+/, "");
	}
	return m[1].replace(/^\n+/, "").replace(/\s+$/, "");
}

/**
 * Registry of built-in skill names.
 *
 * Spec 018-saolei-mcp FR-024: a profile whose `mcp_names` includes a
 * registered entry receives the matching built-in skill body appended to
 * its systemPrompt. FR-025 scope guard: this registry is the ONLY
 * skill-injection surface; it MUST NOT alter the user-created `Skill`
 * proto resource (PromptService CRUD at `projects/game/game.proto`).
 *
 * To add a new built-in skill: author `src/skill/{name}/SKILL.md`, declare
 * it in `data_files` of the `artifact_pkg_js` targets, and add the name
 * here.
 */
const BUILTIN_SKILL_NAMES: readonly string[] = [
	"saolei",
	"memory",
];

/**
 * Load the body of a built-in skill by `mcp_name`.
 *
 * @param name The profile `mcp_names` entry (e.g. `"saolei"`).
 * @returns The skill body (Markdown without frontmatter), or `""` when no
 *   built-in skill is registered for the name (no injection).
 */
export function loadSkillBody(name: string): string {
	if (!BUILTIN_SKILL_NAMES.includes(name)) {
		return "";
	}
	return readSkillBody(name);
}

/**
 * Load and merge the bodies of all built-in skills matching the profile's
 * `mcp_names` (spec 018-saolei-mcp FR-024).
 *
 * Unknown `mcp_names` entries are silently ignored (no registered built-in
 * skill → no injection). Multiple matches are joined by
 * `SKILL_PROMPT_SEPARATOR` in registry order.
 *
 * @param mcpNames The profile's `mcp_names` entries (e.g. `["saolei"]`).
 * @returns The merged skill bodies, or `""` when no skills match.
 */
export function loadSkillsForMcp(mcpNames: string[]): string {
	const bodies: string[] = [];
	for (const name of mcpNames) {
		const body = loadSkillBody(name);
		if (body) {
			bodies.push(body);
		}
	}
	return bodies.join(SKILL_PROMPT_SEPARATOR);
}

/**
 * Append the matching built-in skill bodies to a systemPrompt.
 *
 * Spec 018-saolei-mcp FR-023/024/025 + research.md D9: when `mcpNames`
 * includes a registered built-in skill (currently only `"saolei"`), the
 * skill body is appended to the systemPrompt so the model receives the
 * guidance. When no skill matches, the prompt is returned unchanged.
 *
 * Used by the saolei team graph's player node (`team/player.ts`) at graph
 * build time; the augmented prompt is baked into the player's static system
 * prompt (template-fixed assembly, specs/031-team-template-mode FR-028).
 *
 * @param systemPrompt The original profile systemPrompt.
 * @param mcpNames The profile's `mcp_names` entries.
 * @returns The augmented systemPrompt, or the original when no skill matches.
 */
export function appendSkillBodyToPrompt(
	systemPrompt: string,
	mcpNames: string[],
): string {
	const skillBody = loadSkillsForMcp(mcpNames);
	if (!skillBody) {
		return systemPrompt;
	}
	return systemPrompt + SKILL_PROMPT_SEPARATOR + skillBody;
}
