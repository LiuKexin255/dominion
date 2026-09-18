/**
 * cordis plugin row for the team group-chat primitive: the class-form Service
 * plugin that exposes `ctx.team` (register / reference relay / drain). The
 * member sections, output subscriptions, and buffers are host-row state; the
 * service never drives a member. Contract:
 * specs/059-agent-v2-team-mode/contracts/dsh-plugins.md §1; the member
 * message-source interface (dependency inversion) is
 * specs/065-agent-v2-team-refine/contracts/team-member-source.md §1.
 */

export const name = "team";

export { agentMemberSource, Team as default, Team } from "./team.js";
export { TEAM_SECTION_NAME, TEAM_SECTION_ORDER } from "./team.js";
export type {
  TeamBroadcastSource,
  TeamHandle,
  TeamMemberRegistration,
  TeamMemberSource,
  TeamRegistration,
  TeamSectionTarget,
} from "./team.js";
