import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

// 状态横幅对比度断言（specs/055-agent-v2-ui-fixes/contracts/ui-interactions.md
// §3、data-model.md §3、research.md §2.5 R2）按文件内容读取两份样式文本：
// jsdom 不应用外部样式表，且 vitest 默认 css:false 下 `.css?raw` 导入被替换
// 为空串（src/components/SessionList.test.tsx 顶部记录的 054 交付结论，
// src/theme-fence.test.ts 已按同惯例落地）——readFileSync 读取的正是真实交付
// 的同一份样式，断言对象不变。路径候选覆盖两种执行环境：bazel runfiles
// （js_test cwd = workspace 根，见 projects/game/web/frontend/BUILD.bazel）
// 与包目录下的 vitest CLI。
function loadSrc(rel: string): string {
  for (const base of [
    process.cwd(),
    resolve(process.cwd(), 'projects/game/web/frontend'),
  ]) {
    const path = resolve(base, rel)
    if (existsSync(path)) return readFileSync(path, 'utf8')
  }
  throw new Error(`${rel} not found relative to cwd`)
}

const THEME_CSS = loadSrc('src/theme.css')
const DESIGN_PLATFORM_CSS = loadSrc('src/dsh-theme/design-platform.css')

interface CssBlock {
  selector: string
  decls: Map<string, string>
}

function parseBlocks(css: string): CssBlock[] {
  const blocks: CssBlock[] = []
  // 先剥离块注释：theme.css 规则前带 /* */ 说明注释，会混入 selector 捕获。
  const bare = css.replace(/\/\*[\s\S]*?\*\//g, '')
  for (const m of bare.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    const decls = new Map<string, string>()
    for (const decl of m[2].split(';')) {
      const idx = decl.indexOf(':')
      if (idx > 0) decls.set(decl.slice(0, idx).trim(), decl.slice(idx + 1).trim())
    }
    blocks.push({ selector: m[1].trim(), decls })
  }
  return blocks
}

// web 为深色单一主题（projects/game/web/frontend/index.html 静态
// body[data-ds-dark-theme]，spec Edge Cases"状态横幅的呈现主题"裁定按当前
// 呈现主题验收）：alias 变量在 light/dark 两块解析到不同值，dark 别名块引用
// 的 static 变量取 dark static 块的值——dark 演示主题的解析域即两个 dark 块
// 的并集（本文件内 static 与 alias 变量名不相交，每个名字只在各自主题块出现
// 一次）。
function darkVarScope(css: string): Map<string, string> {
  const scope = new Map<string, string>()
  for (const block of parseBlocks(css)) {
    if (block.selector.includes('[data-ds-dark-theme]')) {
      for (const [name, value] of block.decls) scope.set(name, value)
    }
  }
  return scope
}

const DSW_VARS = darkVarScope(DESIGN_PLATFORM_CSS)

function rootVarScope(css: string): Map<string, string> {
  const root = parseBlocks(css).find((block) => block.selector === ':root')
  if (!root) throw new Error(':root block not found')
  return root.decls
}

// theme.css :root 的 app 级变量优先于 vendored token（app 层在查找序首位；
// 现无同名碰撞，序仅表达层级行为）。
const APP_VARS = rootVarScope(THEME_CSS)

// var() 引用链逐层解引用（alias → static），终止于具体色值字面量。
function resolveVar(name: string): string {
  const seen = new Set<string>()
  let current = name
  for (;;) {
    if (seen.has(current)) throw new Error(`cyclic var reference at ${current}`)
    seen.add(current)
    const value = APP_VARS.get(current) ?? DSW_VARS.get(current)
    if (value === undefined) throw new Error(`undefined variable: ${current}`)
    const inner = /var\(\s*(--[\w-]+)/.exec(value)
    if (!inner) return value
    current = inner[1]
  }
}

// 仅支持不透明 #rgb/#rrggbb：四类横幅引用链终点均为 6 位 hex；带 alpha 的
// 字面量（token 表中的遮罩色）不在解析域内，遇到即抛错（合成背景的对比度
// 需要底色参与，静默丢弃 alpha 会算出假值）。
function hexToRgb(hex: string): [number, number, number] {
  const m = /^#([0-9a-f]+)$/i.exec(hex)
  if (!m) throw new Error(`unsupported color literal: ${hex}`)
  const digits = m[1]
  if (digits.length !== 3 && digits.length !== 6) {
    throw new Error(`unsupported hex digit count in #${digits}`)
  }
  const step = digits.length / 3
  return [0, 1, 2].map((i) =>
    parseInt(digits.slice(i * step, (i + 1) * step).repeat(step === 1 ? 2 : 1), 16),
  ) as [number, number, number]
}

// WCAG 2.2 相对亮度（Understanding SC 1.4.3 Contrast (Minimum)：
// https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html ）——
// L = 0.2126·R + 0.7152·G + 0.0722·B，通道线性化阈值 0.04045。
function relativeLuminance([r, g, b]: [number, number, number]): number {
  const lin = (channel: number) => {
    const s = channel / 255
    return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
  }
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b)
}

// 对比度 = (L1 + 0.05) / (L2 + 0.05)，L1 为较亮者；断言比较不取整（WCAG
// 口径：4.499:1 不满足 4.5:1）。
function contrastRatio(a: string, b: string): number {
  const [l1, l2] = [relativeLuminance(hexToRgb(a)), relativeLuminance(hexToRgb(b))].sort(
    (x, y) => y - x,
  )
  return (l1 + 0.05) / (l2 + 0.05)
}

// 色相（度）用于 FR-006 的红/黄色系区分判定。
function hueDeg([r, g, b]: [number, number, number]): number {
  const max = Math.max(r, g, b)
  const min = Math.min(r, g, b)
  if (max === min) return 0
  const d = max - min
  let h: number
  if (max === r) h = ((g - b) / d) % 6
  else if (max === g) h = (b - r) / d + 2
  else h = (r - g) / d + 4
  h *= 60
  return h < 0 ? h + 360 : h
}

function ruleBody(selector: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const m = new RegExp(`${escaped}\\s*\\{([^}]*)\\}`).exec(THEME_CSS)
  if (!m) throw new Error(`rule ${selector} not found in theme.css`)
  return m[1]
}

function propValue(body: string, prop: string): string {
  const m = new RegExp(`${prop}\\s*:\\s*([^;]+)`).exec(body)
  if (!m) throw new Error(`property ${prop} not found in rule body`)
  return m[1].trim()
}

// 四类横幅（contracts/ui-interactions.md §3.2）：error 组三条规则共用 error
// 组变量，.chat-canceled 用 warn 组。
const BANNERS = [
  { selector: '.chat-error', bgVar: '--app-banner-error-bg', fgVar: '--app-banner-error-fg' },
  { selector: '.presets-error', bgVar: '--app-banner-error-bg', fgVar: '--app-banner-error-fg' },
  {
    selector: '.agent-panel-error',
    bgVar: '--app-banner-error-bg',
    fgVar: '--app-banner-error-fg',
  },
  { selector: '.chat-canceled', bgVar: '--app-banner-warn-bg', fgVar: '--app-banner-warn-fg' },
] as const

const MIN_CONTRAST = 4.5

describe('状态横幅文字-背景对比度（specs/055-agent-v2-ui-fixes/spec.md FR-005，dark 呈现主题）', () => {
  for (const banner of BANNERS) {
    it(`${banner.selector} 配色单一来源化且对比度 ≥ ${MIN_CONTRAST}:1`, () => {
      const body = ruleBody(banner.selector)
      expect(propValue(body, 'background')).toBe(`var(${banner.bgVar})`)
      expect(propValue(body, 'color')).toBe(`var(${banner.fgVar})`)
      const bg = resolveVar(banner.bgVar)
      const fg = resolveVar(banner.fgVar)
      const ratio = contrastRatio(bg, fg)
      expect(
        ratio,
        `${banner.selector}: ${fg} on ${bg} = ${ratio.toFixed(2)}:1`,
      ).toBeGreaterThanOrEqual(MIN_CONTRAST)
    })
  }
})

// data-model.md §3 约束"值必须解析到 vendored token 表中已定义的 token"：
// 四个 --app-banner-* 变量在 theme.css :root 的声明值本身必须是
// var(--dsw-...) 引用形式——防止硬编码 hex 绕过（硬编码值即使通过对比度与
// 色相断言，也偏离 vendored token 单一来源）。
describe('横幅配色变量解析到 vendored token（specs/055-agent-v2-ui-fixes/data-model.md §3）', () => {
  it('四个 --app-banner-* 变量声明值均为 var(--dsw-...) 引用形式', () => {
    for (const name of [
      '--app-banner-error-bg',
      '--app-banner-error-fg',
      '--app-banner-warn-bg',
      '--app-banner-warn-fg',
    ]) {
      const value = APP_VARS.get(name)
      expect(value, `${name} 未在 theme.css :root 定义`).toBeDefined()
      expect(
        value,
        `${name} 声明值 ${value} 非 var(--dsw-...) 引用形式`,
      ).toMatch(/^var\(--dsw-/)
    }
  })
})

// FR-006：错误（红系）与警示（黄系）保持色系可区分——error 组色相落在红色
// 区间、warn 组落在琥珀区间，且两组配色互不相同。
describe('错误/警示色系区分（specs/055-agent-v2-ui-fixes/spec.md FR-006）', () => {
  it('error 组为红色系、warn 组为琥珀色系，两组配色互不重叠', () => {
    const errorBg = hexToRgb(resolveVar('--app-banner-error-bg'))
    const errorFg = hexToRgb(resolveVar('--app-banner-error-fg'))
    const warnBg = hexToRgb(resolveVar('--app-banner-warn-bg'))
    const warnFg = hexToRgb(resolveVar('--app-banner-warn-fg'))
    for (const [label, rgb] of [
      ['error bg', errorBg],
      ['error fg', errorFg],
    ] as const) {
      const h = hueDeg(rgb)
      expect(
        h >= 345 || h <= 20,
        `${label} hue ${h.toFixed(1)}° 不在红色区间`,
      ).toBe(true)
    }
    for (const [label, rgb] of [
      ['warn bg', warnBg],
      ['warn fg', warnFg],
    ] as const) {
      const h = hueDeg(rgb)
      expect(h, `${label} hue ${h.toFixed(1)}° 不在琥珀区间`).toBeGreaterThanOrEqual(30)
      expect(h).toBeLessThanOrEqual(55)
    }
    const errorColors = [errorBg, errorFg].map(String).sort()
    const warnColors = [warnBg, warnFg].map(String).sort()
    expect(errorColors).not.toEqual(warnColors)
  })
})
