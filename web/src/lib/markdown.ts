import DOMPurify from 'dompurify'
import { Marked, type Tokens } from 'marked'

/**
 * Markdown → sanitized HTML. Strict allow-list:
 * - raw HTML in the source is escaped, never interpreted;
 * - images are rendered as their alt text (no remote loads from Markdown);
 * - links keep only http(s)/mailto URLs and open in a new tab with
 *   rel="noopener noreferrer nofollow".
 */

const ALLOWED_TAGS = [
  'p', 'br', 'strong', 'em', 'del', 's', 'code', 'pre', 'blockquote',
  'ul', 'ol', 'li', 'a', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'hr',
  'table', 'thead', 'tbody', 'tr', 'th', 'td', 'input',
]
const ALLOWED_ATTR = ['href', 'title', 'start', 'align', 'type', 'checked', 'disabled']
const SAFE_URL = /^(?:https?:|mailto:)/i

function escapeHtml(s: string) {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;')
}

const marked = new Marked({
  gfm: true,
  breaks: true,
  async: false,
  renderer: {
    html({ text }: Tokens.HTML | Tokens.Tag) {
      return escapeHtml(text)
    },
    image({ text }: Tokens.Image) {
      return escapeHtml(text)
    },
  },
})

let purifier: ReturnType<typeof DOMPurify> | null = null

function getPurifier() {
  if (purifier) return purifier
  const p = DOMPurify(window)
  p.addHook('uponSanitizeAttribute', (node, data) => {
    if (data.attrName === 'href' && !SAFE_URL.test(data.attrValue.trim())) data.keepAttr = false
    // Only GFM task-list checkboxes may survive as <input>.
    if (node.nodeName === 'INPUT' && data.attrName === 'type' && data.attrValue !== 'checkbox') data.keepAttr = false
  })
  p.addHook('afterSanitizeAttributes', (node) => {
    if (node.nodeName === 'A') {
      if (node.hasAttribute('href')) {
        node.setAttribute('target', '_blank')
        node.setAttribute('rel', 'noopener noreferrer nofollow')
      }
    }
    if (node.nodeName === 'INPUT') {
      if (node.getAttribute('type') !== 'checkbox') node.remove()
      else node.setAttribute('disabled', '')
    }
  })
  purifier = p
  return p
}

export function sanitizeHtml(html: string): string {
  return getPurifier().sanitize(html, {
    ALLOWED_TAGS,
    ALLOWED_ATTR,
    ALLOW_DATA_ATTR: false,
    ALLOW_ARIA_ATTR: false,
    ADD_ATTR: ['target', 'rel'],
    FORBID_TAGS: ['style', 'img', 'svg', 'math', 'iframe', 'form', 'script'],
    KEEP_CONTENT: true,
  })
}

export function renderMarkdown(src: string): string {
  const html = marked.parse(src ?? '') as string
  return sanitizeHtml(html)
}
