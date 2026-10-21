import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

// theme.css 的围栏规则断言按文件内容读取：jsdom 不应用外部样式表，且已实证
// vitest 默认 css:false 下 `.css?raw` 导入被替换为空串（bazel lib_test 环境
// 探针返回 length 0，与 src/components/SessionList.test.tsx 顶部记录的 054
// 交付结论一致），故走文件系统——断言对象仍是真实交付的同一份 theme.css。
// 路径候选覆盖两种执行环境：bazel runfiles（js_test cwd = workspace 根，见
// projects/game/web/frontend/BUILD.bazel）与包目录下的 vitest CLI。
function loadThemeCss(): string {
  for (const base of [
    process.cwd(),
    resolve(process.cwd(), 'projects/game/web/frontend'),
  ]) {
    const path = resolve(base, 'src/theme.css')
    if (existsSync(path)) return readFileSync(path, 'utf8')
  }
  throw new Error('theme.css not found relative to cwd')
}

const THEME_CSS = loadThemeCss()

// F1 折叠态布局围栏（specs/055-agent-v2-ui-fixes/contracts/ui-interactions.md
// §2、data-model.md §2）：折叠盒（无 data-expanded）垂直围栏——contain:
// layout + 固定 24px 高，流式增长不影响祖先纵向布局；不用 contain: size
// 的原因见 specs/055-agent-v2-ui-fixes/revisions/fr001-investigation.md
// （size containment 与本地 fit-content 卡片不相容）。
describe('theme.css 思考折叠行围栏（specs/055-agent-v2-ui-fixes/contracts/ui-interactions.md §2）', () => {
  it('折叠态围栏规则存在：contain: layout + height: 24px（F1）', () => {
    const fence = /\.reasoning-row:not\(\[data-expanded\]\)\s*\{([^}]*)\}/.exec(THEME_CSS)
    expect(fence).not.toBeNull()
    expect(fence?.[1]).toContain('contain: layout')
    expect(fence?.[1]).toContain('height: 24px')
  })

  it('running sweep 渐变主题感知化：color-mix 引用 bg-base、无硬编码颜色字面量（F3，契约 §2.3）', () => {
    const sweep = /\.reasoning-row\[data-state='running'\] \.reasoning-row-line::after\s*\{([^}]*)\}/.exec(
      THEME_CSS,
    )
    expect(sweep).not.toBeNull()
    expect(sweep?.[1]).toContain('color-mix(in srgb, var(--dsw-alias-bg-base) 60%, transparent)')
    expect(sweep?.[1]).not.toContain('rgba(15, 18, 22')
  })
})

// FR-001 布局包含（specs/055-agent-v2-ui-fixes/contracts/ui-interactions.md
// §1）：消息区滚动容器定位为绝对定位后代的 containing block，内部 abspos
// 随内容滚动、被滚动容器裁剪，不逃逸到文档层撑大根滚动范围（断点分析见
// specs/055-agent-v2-ui-fixes/revisions/fr001-investigation.md）。
describe('theme.css 消息区布局包含（specs/055-agent-v2-ui-fixes/contracts/ui-interactions.md §1）', () => {
  it('.chat-messages 含 position: relative（滚动容器承接内部绝对定位）', () => {
    const rule = /^\.chat-messages\s*\{([^}]*)\}/m.exec(THEME_CSS)
    expect(rule).not.toBeNull()
    expect(rule?.[1]).toContain('position: relative')
  })
})

// code 块贴合内容宽（用户裁定：代码块不强行与 fit-content 卡片同宽）+
// 右侧内边距校准（44px = 左侧 16px + "复制"文案宽，用户裁定）；断点数据
// 见 specs/055-agent-v2-ui-fixes/revisions/fr001-investigation.md。
describe('theme.css code 块宽度（specs/055-agent-v2-ui-fixes/revisions/fr001-investigation.md）', () => {
  it('.msg-agent 内 code 块包装贴合内容宽、受卡片宽约束并带右侧内边距', () => {
    const rule = /\.msg-agent \.md-code-block\s*\{([^}]*)\}/.exec(THEME_CSS)
    expect(rule).not.toBeNull()
    expect(rule?.[1]).toContain('width: fit-content')
    expect(rule?.[1]).toContain('max-width: 100%')
    expect(rule?.[1]).toContain('padding-right: 44px')
  })
})

// 展开体容器不自带 white-space/字体/颜色（MarkdownText variant="compact"
// 承载次级排版）：渲染器在块级元素之间与松散 li 内部交织字面 \n 文本节点，
// 仅在正常 white-space 折叠下不可见（包内 wrapBlockChildren 实现注释）；
// pre-wrap 会把它们渲染成强制换行——段落间多一个空行、松散列表序号独占
// 一行。`.markdown` 根类自身的 font/color 直接声明亦优先于 wrapper 继承值，
// 此处写字号/颜色本就不生效。
describe('theme.css 思考展开体容器（对齐上游 .thinkBody）', () => {
  it('.reasoning-body 只声明缩进与最小宽，不声明 white-space/word-break/字体颜色', () => {
    const rule = /^\.reasoning-body\s*\{([^}]*)\}/m.exec(THEME_CSS)
    expect(rule).not.toBeNull()
    expect(rule?.[1]).toContain('padding: 4px 0 4px 22px')
    expect(rule?.[1]).toContain('min-width: 0')
    expect(rule?.[1]).not.toMatch(/white-space|word-break|font-size|color/)
  })
})
