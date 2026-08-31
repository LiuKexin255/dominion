/**
 * cordis plugin entry for the saolei tools plugin: registers the
 * saolei_init/saolei_operate/saolei_remain tools and the saolei:guidance
 * prompt section, delegating execution to the host-level `saoleiGame`
 * service provided by the saolei-loop plugin.
 * `name` is the plugin identifier the cordis Loader resolves from the
 * composition manifest.
 * Contract: specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md §3.
 */

export const name = "saolei";
