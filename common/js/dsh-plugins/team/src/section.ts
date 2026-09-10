/**
 * The team section text: the team-level prompt fact team members receive.
 * Ownership boundary (specs/059-agent-v2-team-mode/research.md R2): the
 * section carries only team-level facts — goal, a third-person one-line
 * roster, and the broadcast format convention — and MUST NOT state a member's
 * first-person identity (that is the preset persona's job). Registration
 * target and order band: specs/059-agent-v2-team-mode/contracts/dsh-plugins.md
 * §1 item 1 (per-member agent scope, order 1–49); the format wording follows
 * survey/deepseek-harness-team-mode.md §5.3 layer 3.
 */

/** One roster entry: the member's role label and third-person one-liner. */
export interface TeamSectionMember {
  readonly role: string;
  readonly summary: string;
}

/** The facts the team section renders from (the `register` parameters). */
export interface TeamSectionInput {
  readonly goal: string;
  readonly members: readonly TeamSectionMember[];
}

/**
 * Render the section. The same text is registered on every member (content is
 * team-level, not per-member), so members share one stable prefix.
 */
export function renderTeamSection(input: TeamSectionInput): string {
  const roster = input.members
    .map((member) => `- [${member.role}] ${member.summary}`)
    .join("\n");
  return [
    "## 团队",
    "",
    `你所在的团队目标：${input.goal}`,
    "",
    "团队成员：",
    roster,
    "",
    "其他成员的输出会以群聊广播消息发送给你，格式为：首行 `[角色] 摘要`（工具调用为 `[角色] 工具调用 <工具名>`），其后是标签对包裹的原样正文。",
    "- 发言：`<角色-message>…</角色-message>` 之间为发言原文；",
    "- 工具调用：`<角色-tool-call>…</角色-tool-call>` 之间依次为 `tool:`、`args:`、`result:`，参数与结果均保持原文（不截断、不摘要）。",
    "消息正文中的 @角色 是发送者的表达：广播面向全体成员，系统不按 @ 定向投递。",
  ].join("\n");
}
