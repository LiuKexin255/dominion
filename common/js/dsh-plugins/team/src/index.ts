/**
 * cordis plugin row for the team group-chat primitive: the class-form Service
 * plugin that exposes `ctx.team` (register / reference relay / drain). The
 * member sections, output subscriptions, and buffers are host-row state; the
 * service never drives a member. Contract:
 * specs/059-agent-v2-team-mode/contracts/dsh-plugins.md §1.
 */

export const name = "team";

export { TeamService as default, TeamService } from "./team.js";
export { TEAM_SECTION_NAME, TEAM_SECTION_ORDER } from "./team.js";
export type {
  TeamBroadcastSource,
  TeamHandle,
  TeamMemberRegistration,
  TeamRegistration,
} from "./team.js";
