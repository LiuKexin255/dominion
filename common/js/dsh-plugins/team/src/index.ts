/**
 * cordis plugin entry for the team group-chat primitive. This entry is the
 * minimal composable placeholder: an empty apply with no service
 * requirements, so the row loads in any host composition. The ctx.team
 * service contract (register / reference relay / drain) is
 * specs/059-agent-v2-team-mode/contracts/dsh-plugins.md §1.
 */

import type { Context } from "@deepseek-ai/cordis";

export const name = "team";

/** Host-row plugin: no service requirements. */
export const inject: string[] = [];

export function apply(_ctx: Context): void {}
