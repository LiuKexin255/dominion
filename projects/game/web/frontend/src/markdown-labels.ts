// MarkdownText 的 labels 为必填（fence 复制按钮与脚注 chrome 的本地化文案由
// 宿主提供，包内无语言回退）。本应用无本地化需求，共用这一份模块级常量：
// 引用稳定，避免流式期间因新对象身份而丢弃渲染缓存（包 README
// "Localizing copy" 与 "Streaming markdown" 两节：
// https://www.npmjs.com/package/@deepseek-ai/dsh-client-ui-primitives ）。
import type { MarkdownLabels } from '@deepseek-ai/dsh-client-ui-primitives'

export const MARKDOWN_LABELS: MarkdownLabels = {
  code: { copyLabel: '复制', copiedLabel: '已复制' },
  footnotes: '脚注',
}
