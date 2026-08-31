/**
 * cordis plugin entry for the saolei agent loop: the concrete Agent factory
 * and driver replacing the official agent-loop, plus the host-level
 * `saoleiGame` service surface consumed by the saolei tools plugin.
 * `name` is the plugin identifier the cordis Loader resolves from the
 * composition manifest.
 * Contract: specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md §2.
 */

export const name = "saolei-loop";
