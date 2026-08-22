/**
 * Snapshot identifier of the dsh framework core baseline materialized by this
 * package.
 *
 * The pinned closure (11 packages) is declared in
 * third_party/dsh/core/package.json: the @deepseek-ai/dsh-* packages lock the
 * 0.1.1-rc.2 line; the cordis family and node-addon-require-builtin have no
 * 0.1.1-rc.* line on the registry, so they pin the stable lines interlocked
 * by peer ranges — cordis via dsh-app-boot@0.1.1-rc.2 peers, and
 * node-addon-require-builtin via cordis-plugin-loader@1.0.2 peers
 * (specs/047-dsh-chat-demo/research.md D6, D10).
 */
export const DSH_CORE_SNAPSHOT = 'dsh-core@0.1.1-rc.2';
