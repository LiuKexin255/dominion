/**
 * Snapshot identifier of the dsh framework core baseline materialized by this
 * package.
 *
 * The pinned closure (11 packages) is declared in
 * third_party/dsh/core/package.json: the @deepseek-ai/dsh-* packages lock the
 * 0.1.7-rc.2 line; the cordis family and node-addon-require-builtin have no
 * 0.1.7-rc.* line on the registry, so they pin the exact versions interlocked
 * by the 0.1.7 peer ranges — cordis 4.0.4, cordis-plugin-loader 1.0.5,
 * cordis-plugin-include 1.0.9, cordis-plugin-group 1.0.4, cordis-plugin-timer
 * 1.1.6, node-addon-require-builtin 0.1.6
 * (specs/070-dsh-upgrade/research.md D1).
 */
export const DSH_CORE_SNAPSHOT = 'dsh-core@0.1.7-rc.2';
