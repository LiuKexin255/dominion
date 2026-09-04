// FR-022 消费覆盖断言：运行时消费的全部 `--dsw-*` 必须由 vendored token
// sheets（src/dsh-theme/）定义。设计依据
// specs/054-agent-v2-bugfixes/revisions/phase10-theme-css-carrier.md §3.6/§4。
// 读取面均为既有稳定输入：primitives 是运行时依赖（vitest data 已含其
// node_modules target），sheets 是 src 源文件（data glob 已含）——失败即
// primitives catalog 升级引入了 sheets 未跟随时的新消费 token，是升级信号
// 而非自动同步。
import { existsSync, readdirSync, readFileSync } from 'node:fs'
import { extname, join, resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

// 文件定位沿用 SessionList.test.tsx 的 cwd 候选模式：bazel runfiles（js_test
// cwd = workspace 根，见 projects/game/web/frontend/BUILD.bazel）与包目录下的
// vitest CLI 两种执行环境。
function resolveFirst(rel: string): string {
  for (const base of [
    process.cwd(),
    resolve(process.cwd(), 'projects/game/web/frontend'),
  ]) {
    const path = resolve(base, rel)
    if (existsSync(path)) return path
  }
  throw new Error(`${rel} not found relative to cwd`)
}

const UPGRADE_HINT = 'see src/dsh-theme/README.md for the manual upgrade flow'

function collectCssFiles(dir: string): string[] {
  const out: string[] = []
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name)
    if (entry.isDirectory()) out.push(...collectCssFiles(path))
    else if (extname(entry.name) === '.css') out.push(path)
  }
  return out
}

describe('dsh-theme FR-022 消费覆盖', () => {
  it('primitives 与 theme.css 消费的 --dsw-* 全部由 vendored sheets 定义', () => {
    // defined：vendored sheets（含 index.css 无妨——@import 行不构成
    // `--dsw-*:` 声明形态）。
    const defined = new Set<string>()
    for (const file of collectCssFiles(resolveFirst('src/dsh-theme'))) {
      for (const m of readFileSync(file, 'utf8').matchAll(/(--dsw-[a-z0-9-]+)\s*:/g)) {
        defined.add(m[1])
      }
    }
    // consumed：primitives 全部 lib css + 自有 theme.css 的 var() 消费面。
    const consumed = new Set<string>()
    for (const file of collectCssFiles(
      resolveFirst('node_modules/@deepseek-ai/dsh-client-ui-primitives/lib'),
    )) {
      for (const m of readFileSync(file, 'utf8').matchAll(
        /var\(\s*(--dsw-[a-z0-9-]+)/g,
      )) {
        consumed.add(m[1])
      }
    }
    for (const m of readFileSync(resolveFirst('src/theme.css'), 'utf8').matchAll(
      /var\(\s*(--dsw-[a-z0-9-]+)/g,
    )) {
      consumed.add(m[1])
    }
    // 豁免 --dsw-hovercard-bg：仅 HoverCard.module.css 消费，本应用未使用
    // HoverCard，官方该 token 由 ui-layout 侧定义、不在 ui-theme sheets 内
    // （revision §1.3）。
    const exempt = new Set(['--dsw-hovercard-bg'])
    const missing = [...consumed].filter((t) => !defined.has(t) && !exempt.has(t))
    expect([...missing].sort(), UPGRADE_HINT).toEqual([])
  })
})
