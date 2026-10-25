// 场景词扫描 helper：wiki 与 memory 是场景无关的基础能力，其 web 管理视图的
// 用户可见文案不得出现场景角色词与具体场景示例
// （specs/071-memory-preset-scope/spec.md FR-016/FR-017；验收锚点
// specs/071-memory-preset-scope/contracts/web-memory-view.md §5 锚点 2「渲染输出
// 含空态/占位/错误文案」）。
//
// 单点定义、由 MemoryView 与 WikiView 的组件测试共同复用：词表分散在各测试
// 文件会使两个视图的扫描范围漂移，也让新增场景词需要多处同步。
//
// 词表 = 场景角色词（planner/player）+ 场景示例词（saolei/扫雷/sts2/杀戮尖塔）。
// preset 视图与对话页把 role / 成员角色作为资源数据呈现，属 FR-017 的保留面，
// 不在扫描范围。
//
// 场景词随合法模板集合扩容而覆盖全部场景（当前 {saolei, sts2}），依据
// specs/079-sts2-template-scaffold/spec.md FR-005 与
// specs/079-sts2-template-scaffold/contracts/scene-dispatch.md §5。

export const SCENE_WORDS = [
  'planner',
  'player',
  'saolei',
  '扫雷',
  'sts2',
  '杀戮尖塔',
] as const

// 用户可见文案的承载面：文本节点之外还有表单占位与无障碍标签——管理视图的示例
// 文案常以 placeholder 形态承载（specs/069-wiki-template-web/contracts/
// web-wiki-view.md §3.3 的标题占位即为一例）。
const COPY_ATTRIBUTES = ['placeholder', 'aria-label', 'title'] as const

// findSceneWords returns the scenario words present in `text` (SCENE_WORDS
// order, each word once); an empty array means the text is scenario-neutral.
// 大小写不敏感：这些词随 wire 值（role 字符串）传播，文案中可能出现不同大小写。
export function findSceneWords(text: string): string[] {
  const haystack = text.toLowerCase()
  return SCENE_WORDS.filter((word) => haystack.includes(word.toLowerCase()))
}

// findSceneWordsIn scans one rendered subtree's user-visible copy: text content
// plus placeholder/aria-label/title attributes. An empty array means the
// subtree renders scenario-neutral copy.
export function findSceneWordsIn(root: HTMLElement): string[] {
  const copy: string[] = [root.textContent ?? '']
  const selector = COPY_ATTRIBUTES.map((attribute) => `[${attribute}]`).join(', ')
  for (const node of root.querySelectorAll(selector)) {
    for (const attribute of COPY_ATTRIBUTES) {
      const value = node.getAttribute(attribute)
      if (value !== null) copy.push(value)
    }
  }
  return findSceneWords(copy.join('\n'))
}
