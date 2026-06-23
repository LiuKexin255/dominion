import { marked } from "marked";
import DOMPurify from "dompurify";

export function renderMarkdown(input: string): string {
  const raw = marked.parse(input, { async: false }) as string;
  return DOMPurify.sanitize(raw, {
    ALLOWED_TAGS: ['p', 'br', 'strong', 'em', 'code', 'pre', 'ul', 'ol', 'li', 'blockquote', 'a'],
    ALLOWED_ATTR: ['href', 'title'],
  });
}
