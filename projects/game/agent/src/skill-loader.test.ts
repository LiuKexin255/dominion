/**
 * skill-loader.test.ts — Tests for the built-in skill loader.
 *
 * Coverage:
 *   - SKILL.md format contract (FR-023): the authored
 *     `src/skill/saolei/SKILL.md` has a frontmatter `name` matching the
 *     folder and a non-empty `description`.
 *   - Registry lookup (FR-024): `loadSkillBody` / `loadSkillsForMcp` return
 *     the saolei body for `"saolei"` (read from disk via fs.readFileSync),
 *     `""` for unknown names.
 *   - Prompt append (FR-023/024/025, research.md D9):
 *     `appendSkillBodyToPrompt` injects only when `mcpNames` matches a
 *     registered built-in skill; non-matching profiles are unchanged.
 *
 * Pattern (style/javascript.md §测试): pure functions + a real fs.readFileSync
 * against the source tree (the `.md` is in the vitest_test `data`); no
 * `vi.mock` of fs or of skill-loader internals.
 */

import * as fs from "node:fs";
import * as path from "node:path";

import { describe, expect, it } from "vitest";

import {
	appendSkillBodyToPrompt,
	loadSkillBody,
	loadSkillsForMcp,
	SKILL_PROMPT_SEPARATOR,
} from "./skill-loader";

// ===========================================================================
// SKILL.md format contract — FR-023
// ===========================================================================

describe("SKILL.md format contract (FR-023)", () => {
	it("SKILL.md frontmatter name matches the folder name (format contract)", () => {
		// specs/020-agent-resources-layout/contracts/skill-md-format.md:
		// the frontmatter `name` MUST equal the parent folder name.
		const mdPath = path.join(__dirname, "skill", "saolei", "SKILL.md");
		const md = fs.readFileSync(mdPath, "utf8");
		const m = md.match(/^---\n[\s\S]*?name:\s*(\S+)\n/);
		expect(m).not.toBeNull();
		expect(m![1]).toBe("saolei");
	});

	it("SKILL.md has a non-empty description in frontmatter", () => {
		const mdPath = path.join(__dirname, "skill", "saolei", "SKILL.md");
		const md = fs.readFileSync(mdPath, "utf8");
		const m = md.match(/^---\n[\s\S]*?description:\s*(.+?)\n/);
		expect(m).not.toBeNull();
		expect(m![1].trim().length).toBeGreaterThan(0);
	});

	// spec 039 FR-020 (T015b): the memory skill follows the same format
	// contract as saolei — folder name === frontmatter name, non-empty
	// description.
	it("memory SKILL.md frontmatter name matches the folder name (format contract)", () => {
		const mdPath = path.join(__dirname, "skill", "memory", "SKILL.md");
		const md = fs.readFileSync(mdPath, "utf8");
		const m = md.match(/^---\n[\s\S]*?name:\s*(\S+)\n/);
		expect(m).not.toBeNull();
		expect(m![1]).toBe("memory");
	});

	it("memory SKILL.md has a non-empty description in frontmatter", () => {
		const mdPath = path.join(__dirname, "skill", "memory", "SKILL.md");
		const md = fs.readFileSync(mdPath, "utf8");
		const m = md.match(/^---\n[\s\S]*?description:\s*(.+?)\n/);
		expect(m).not.toBeNull();
		expect(m![1].trim().length).toBeGreaterThan(0);
	});
});

// ===========================================================================
// Registry lookup — FR-024
// ===========================================================================

describe("loadSkillBody (FR-024 registry)", () => {
	it("returns the saolei body for 'saolei'", () => {
		const body = loadSkillBody("saolei");
		expect(body.length).toBeGreaterThan(0);
		// A stable marker that will always be present as long as the skill
		// documents the heading + tool set.
		expect(body).toContain("# saolei");
		expect(body).toContain("saolei_init");
	});

	it("returns '' for an unknown mcp_name (no built-in skill)", () => {
		expect(loadSkillBody("nonexistent-skill")).toBe("");
	});

	it("returns '' for an empty name", () => {
		expect(loadSkillBody("")).toBe("");
	});

	it("returns a non-empty memory body for 'memory' (spec 039 FR-020)", () => {
		const body = loadSkillBody("memory");
		expect(body.length).toBeGreaterThan(0);
		// Stable markers: the body documents the single hermes-style tool and
		// its action/old_text surface (memory-skill-contract §3).
		expect(body).toContain("# memory");
		expect(body).toContain("old_text");
		expect(body).toContain("frozen snapshot");
	});
});

describe("loadSkillsForMcp (FR-024 multi-match merge)", () => {
	it("returns the saolei body when mcpNames includes 'saolei'", () => {
		const body = loadSkillsForMcp(["saolei"]);
		expect(body).toBe(loadSkillBody("saolei"));
	});

	it("returns '' when mcpNames is empty", () => {
		expect(loadSkillsForMcp([])).toBe("");
	});

	it("returns '' when no mcpNames match a registered skill", () => {
		expect(loadSkillsForMcp(["unknown", "also-unknown"])).toBe("");
	});

	it("ignores unknown entries and returns the matching skill body", () => {
		const body = loadSkillsForMcp(["unknown", "saolei", "also-unknown"]);
		expect(body).toBe(loadSkillBody("saolei"));
	});
});

// ===========================================================================
// Prompt append — FR-023/024/025, research.md D9
// ===========================================================================

describe("appendSkillBodyToPrompt (FR-023/024/025, research.md D9)", () => {
	it("appends the saolei skill body when mcpNames includes 'saolei'", () => {
		const result = appendSkillBodyToPrompt("base-prompt", ["saolei"]);
		expect(result).toBe(
			"base-prompt" + SKILL_PROMPT_SEPARATOR + loadSkillBody("saolei"),
		);
		// The separator + heading are present, proving the body was appended
		// as a skill section rather than concatenated raw.
		expect(result).toContain(SKILL_PROMPT_SEPARATOR + "# saolei");
	});

	it("returns the prompt unchanged when mcpNames is empty", () => {
		expect(appendSkillBodyToPrompt("base-prompt", [])).toBe("base-prompt");
	});

	it("returns the prompt unchanged when mcpNames has no match", () => {
		expect(appendSkillBodyToPrompt("base-prompt", ["unknown"])).toBe(
			"base-prompt",
		);
	});

	it("appends only the matching skill body (ignores unknown mcpNames)", () => {
		const result = appendSkillBodyToPrompt("p", ["unknown", "saolei"]);
		expect(result).toBe(
			"p" + SKILL_PROMPT_SEPARATOR + loadSkillBody("saolei"),
		);
	});

	it("appends saolei + memory bodies in registry order (039 FR-020)", () => {
		const result = appendSkillBodyToPrompt("p", ["saolei", "memory"]);
		expect(result).toBe(
			"p" +
				SKILL_PROMPT_SEPARATOR +
				loadSkillBody("saolei") +
				SKILL_PROMPT_SEPARATOR +
				loadSkillBody("memory"),
		);
		expect(result).toContain(SKILL_PROMPT_SEPARATOR + "# memory");
	});

	it("preserves the original prompt prefix exactly (no truncation)", () => {
		const prompt = "You are a minesweeper agent. Follow the rules.";
		const result = appendSkillBodyToPrompt(prompt, ["saolei"]);
		expect(result.startsWith(prompt)).toBe(true);
	});

	it("does not inject the skill body for a non-saolei profile (FR-024 negative)", () => {
		// A profile with NO mcp_names, or with mcp_names that do NOT include
		// saolei, MUST NOT receive the skill body (FR-024 negative branch,
		// quickstart.md Scenario 5).
		const result = appendSkillBodyToPrompt("base-prompt", []);
		expect(result).not.toContain("# saolei");
		expect(result).not.toContain(SKILL_PROMPT_SEPARATOR);
	});
});
