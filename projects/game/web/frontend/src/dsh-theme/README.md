# src/dsh-theme：官方 token sheets（vendored 源文件）

本目录是 [dsh-web](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/client/ui-theme)
官方 token sheets 的源码形态 vendored 副本，是 `--dsw-*` token 的唯一权威
（`theme.css` 不定义任何 `--dsw-*`）。载体裁定见
`specs/054-agent-v2-bugfixes/revisions/phase10-theme-css-carrier.md` §2–3。

## 来源与版本

- 来源包：[`@deepseek-ai/dsh-client-ui-theme`](https://www.npmjs.com/package/@deepseek-ai/dsh-client-ui-theme)，
  vendored 版本 **0.1.1-rc.2**（与 `pnpm-workspace.yaml` catalog 的
  `@deepseek-ai/dsh-client-ui-primitives` 条目同线）；
- 许可证：MIT；上游仓库
  <https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/client/ui-theme>。

## 获取源实态

npm tarball **不含独立的 css 文件**（README 所述 `src/styles/` 路径与 tarball
实态不符）：五个 sheet 以字符串常量内嵌于 `lib/client.js`，形如

```
//#region \0dsh-inline-css:...src/styles/<name>.css.mjs
var <ident> = "<json-escaped css>"
```

引入顺序由包内 `STYLES` 数组（`[[name, ident], ...]`）声明，0.1.1-rc.2 的官方
顺序为：base → design-platform → scrollbar → gradient-shadow-text → shiki。
GitHub master 分支的 css 已漂移（新增 sheet、字节不一致），**不可替代 npm
tarball 作为获取源**。

## 人工升级步骤

1. `npm pack @deepseek-ai/dsh-client-ui-theme@<目标版本>` 并解包；
2. 按 region 锚点（形态见上）从解包产物的 `lib/client.js` 提取全部 sheet
   （JSON 反转义后按字节落盘），覆盖本目录同名 css——sheet 集变化（如
   0.1.2-rc.1 新增 `corner-shape.css`）须一并评估是否引入；
3. 按新版本包内 `STYLES` 数组核对 `index.css` 的 `@import` 顺序；
4. 更新本 README 的版本记录；
5. diff review 后提交。

升级后运行 `bazel test //projects/game/web/frontend/...`：
`src/dsh-theme.test.ts` 的 FR-022 消费覆盖断言会在 vendored sheets 未跟上
primitives 新消费 token 时失败（该断言是升级信号，不是自动同步）。
