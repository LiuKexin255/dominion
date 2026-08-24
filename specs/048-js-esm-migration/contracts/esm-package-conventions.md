# Contract: 工作区 JS 包 ESM 规范（esm-package-conventions）

**Feature**: [spec.md](../spec.md) | **Date**: 2026-08-24

这是工作区每个 JS/TS 包在 ESM 终态下必须满足的**包级契约**——Q3 用户故事（统一模块规范）的机器可审计定义，也是 SC-001/SC-004 静态审计的判据。依据：[research.md](../research.md) R1/R2/R5。

## 1. 包声明

- `package.json` MUST 含 `"type": "module"`（唯一例外：仓库根 `package.json`，非 workspace 包且无 JS 源）。
- 库包（被工作区消费）的 `exports["."]` 保持 `types` + `default` 双条件，指向 `./src/index.js`（saolei-board 为 `./src/core/index.js`）——路径不变，产物语义由 CJS 变为 ESM；**导出 API 名称与语义不变**（spec US2-2）。
- bin 入口（`@dominion/game-saolei-board` 的 `saolei-recognize`）在 `"type": "module"` 下自动按 ESM 解析，路径不变。

## 2. 编译配置（tsconfig 与 .swcrc 锁步）

- `tsconfig.json`：`"module": "nodenext"`（隐含 `moduleResolution: nodenext`、`esModuleInterop`）；`target: ES2020`；消费工作区依赖的 `paths` MUST 指向 `"<dep>/src/index.js"`（TS 扩展名替换定位到 `.ts` 源码做类型检查）。
- `.swcrc`：`"module": {"type": "es6", "preserveImportMeta": true}`；`jsc.target: es2020`。
- 两文件 MUST 同步变更（swc 不读 tsconfig）；tsconfig `module` 与 `.swcrc` `module.type` 的组合不一致视为契约违反。

## 3. 源码书写规则

- 相对导入 MUST 带 `.js` 扩展名（`./merge.js`）；禁止无扩展名与目录导入。
- 禁止 `__dirname`/`__filename`/`require()` 直用/`module.exports`；资源定位使用 `import.meta.dirname`（目录）与 `import.meta.url`（URL 锚点）。
- 对 CJS 第三方依赖：**default import 为默认约定**；具名导入仅允许用于 [research.md R5](../research.md#r5) 表中标注"具名导入可用"的包（`@grpc/grpc-js`、`@grpc/proto-loader`、`mongodb`、`pngjs`）。
- 类型再导出必须显式 `export type`（swc 类型擦除歧义规避，rules_ts #398）。
- 不得引入对 npm 包 `const enum` 的消费。

## 4. 打包与运行

- `artifact_pkg_js` 的 `package_json` 属性（label 类型，默认 `"package.json"`，**不可关闭**）将服务 manifest 打包至 `dominion/{app}/{service}/package.json`，承载服务根模块格式判定。两道构建期门禁为终态执行语义：① 存在性——目标所在包缺 `package.json` 时构建分析期失败；② 内容——打包 action 断言 manifest 含 `"type": "module"`（ESM-only 构建，CJS 服务产物不再支持；内容门禁随最后一个服务翻转完成后启用）。服务 target 使用默认值即可，MUST NOT 经 `data_files` 重复携带 `package.json`（与宏默认打包的目的地冲突）。
- `js_binary` target 的 data/runfiles MUST 使包自身 `package.json` 与编译产物同根可见（runfiles 最近 package.json 判定）。
- 服务入口保持 `src/bootstrap.js`（扩展名不变）；bootstrap 两段式形态（OTel 装配 → 动态 import server）MUST 保持（见 [otel-instrumentation-esm-contract.md](otel-instrumentation-esm-contract.md) §2）。

## 5. 测试

- 测试与源码同为 ESM；`vitest_test` data MUST 继续只含 raw `.ts` 源（不混入编译 `:lib`，双实例不变量沿用 `specs/019-js-test-reliability/contracts/run-vitest-shim.md`）。
- `require` 的唯一豁免：RITM 插桩验证场景，以 `createRequire(import.meta.url)` 形式书写（见 otel 契约 §3）。

## 6. 审计判据（SC-001/SC-004）

本节命令集为静态审计的**唯一权威版本**（quickstart 场景 5、data-model §4 与 tasks.md T021 均引用本节，不另行复制）。终态下以下查询零命中（豁免项除外）：

```bash
# CJS 编译配置残留
rg '"module": *"commonjs"' -g 'tsconfig.json' common projects experimental third_party
rg '"type": *"commonjs"' -g '.swcrc' common projects experimental third_party
# 源码 CJS 惯用法（前端包不在审计范围）
rg '__dirname|__filename|module\.exports' -g '*.ts' common/js projects/game experimental --glob '!frontend'
# require( 直用：生产源码（排除测试文件与前端包）
rg 'require\(' -g '*.ts' --glob '!*.test.ts' --glob '!frontend' common/js projects/game experimental third_party
# 测试文件 require(：唯一豁免 common/js/grpc/otel/src/index.test.ts（createRequire 场景，otel 契约 §3）
rg 'require\(' -g '*.test.ts' --glob '!frontend' -g '!common/js/grpc/otel/src/index.test.ts' common/js projects experimental third_party
# workspace 包缺 "type": "module"（唯一例外：仓库根 package.json，非 workspace 包且无 JS 源）
rg -L '"type": *"module"' -g 'package.json' common projects experimental third_party
```

前端包 `projects/game/desktop/frontend` 不在本契约审计范围（自有 bundler 体系，`moduleResolution: bundler` 合法）；其不变量是"持续可用"（FR-002），由既有 build/test target 回归保证。
