/**
 * cordis plugin entry for the desktop operation bridge: binds desktop
 * connections to agent sessions and dispatches operations/screenshots.
 * `name` is the plugin identifier the cordis Loader resolves from the
 * composition manifest.
 * Contract: specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md §2,
 * package shape: specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md §1.
 */

export const name = "desktop-bridge";
