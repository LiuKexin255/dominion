/**
 * profile-guard.ts — Pure profile-mismatch guard for turn entry.
 *
 * Decides whether an inbound user turn must be rejected because its resolved
 * profile name differs from the bound adapter's. Extracted as a pure function
 * so the guard rule is unit-testable in isolation and reusable by the Connect
 * content handler.
 *
 * Rule (specs/021-agent-session-resync/data-model.md §5;
 * specs/021-agent-session-resync/contracts/agent-session-lifecycle-contract.md §3):
 * reject ONLY when an adapter is bound, has a non-empty active profile name,
 * and that name differs from the turn's effective profile name. When unbound
 * (first turn / post-Refresh) any profile is accepted so the adapter can be
 * built for it.
 */

/**
 * Return true when the turn must be rejected before it enters the agent.
 *
 * @param activeProfileName    The bound adapter's profile name (null when unbound).
 * @param isBound              Whether an adapter is currently bound.
 * @param effectiveProfileName The turn's resolved profile name (after the
 *                             empty⇒bound fallback applied by the handler).
 */
export function shouldRejectProfile(
  activeProfileName: string | null,
  isBound: boolean,
  effectiveProfileName: string,
): boolean {
  if (!isBound) {
    return false;
  }
  if (!activeProfileName) {
    return false;
  }
  return activeProfileName !== effectiveProfileName;
}
