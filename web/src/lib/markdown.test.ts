import { describe, expect, it } from 'vitest'
import { renderMarkdown } from './markdown'

function dom(html: string) {
  const d = document.createElement('div')
  d.innerHTML = html
  return d
}

const XSS = [
  '<script>alert(1)</script>',
  '<img src=x onerror=alert(1)>',
  '<svg/onload=alert(1)>',
  '<iframe src="javascript:alert(1)"></iframe>',
  '[click](javascript:alert(1))',
  '[click](JaVaScRiPt:alert(1))',
  '[click](data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==)',
  '[click](vbscript:msgbox(1))',
  '<a href="javascript:alert(1)">x</a>',
  '<div style="background:url(javascript:alert(1))">x</div>',
  '<math><mi xlink:href="javascript:alert(1)">x</mi></math>',
  '![x](https://evil.example/track.png)',
  '![x" onerror="alert(1)](x)',
  '<details open ontoggle=alert(1)>',
  '<form action="https://evil"><input name=x></form>',
  '[x](https://ok.example "t\\" onmouseover=\\"alert(1)")',
  '<a href="https://ok" onclick="alert(1)">ok</a>',
  '``<img src=x onerror=alert(1)>``',
  '<style>body{display:none}</style>',
  '<base href="https://evil/">',
]

describe('renderMarkdown sanitizer', () => {
  it.each(XSS)('neutralises %s', (payload) => {
    const html = renderMarkdown(payload)
    const d = dom(html)
    expect(d.querySelectorAll('script,img,svg,iframe,math,style,form,base,details').length).toBe(0)
    for (const el of d.querySelectorAll('*')) {
      for (const attr of el.attributes) {
        expect(attr.name.startsWith('on')).toBe(false)
        expect(attr.name).not.toBe('style')
        expect(attr.name).not.toBe('src')
      }
    }
    for (const a of d.querySelectorAll('a[href]')) {
      expect(a.getAttribute('href')).toMatch(/^(https?:|mailto:)/i)
    }
  })

  it('renders basic Markdown', () => {
    const d = dom(renderMarkdown('**bold** _em_ `code`\n\n- a\n- b'))
    expect(d.querySelector('strong')?.textContent).toBe('bold')
    expect(d.querySelector('em')?.textContent).toBe('em')
    expect(d.querySelector('code')?.textContent).toBe('code')
    expect(d.querySelectorAll('li')).toHaveLength(2)
  })

  it('hardens links', () => {
    const d = dom(renderMarkdown('[site](https://example.com) and <https://auto.example> and [mail](mailto:a@b.c)'))
    const links = [...d.querySelectorAll('a')]
    expect(links).toHaveLength(3)
    for (const a of links) {
      expect(a.getAttribute('target')).toBe('_blank')
      expect(a.getAttribute('rel')).toBe('noopener noreferrer nofollow')
    }
  })

  it('escapes raw HTML instead of rendering it', () => {
    const d = dom(renderMarkdown('<b>not bold</b>'))
    expect(d.querySelector('b')).toBeNull()
    expect(d.textContent).toContain('<b>not bold</b>')
  })

  it('renders images as alt text only', () => {
    const d = dom(renderMarkdown('![a map](https://example.com/map.png)'))
    expect(d.querySelector('img')).toBeNull()
    expect(d.textContent).toContain('a map')
  })

  it('drops links with relative or unknown schemes', () => {
    const d = dom(renderMarkdown('[rel](/api/me) [ftp](ftp://x)'))
    expect(d.querySelectorAll('a[href]')).toHaveLength(0)
  })
})
