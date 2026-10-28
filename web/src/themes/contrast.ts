/** WCAG 2.x relative-luminance contrast helpers for hex colors. */

export function parseHex(hex: string): [number, number, number] | null {
  const m = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(hex.trim())
  if (!m) return null
  let h = m[1] as string
  if (h.length === 3) h = h.replace(/./g, (c) => c + c)
  const n = parseInt(h, 16)
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255]
}

export function toHex([r, g, b]: [number, number, number]) {
  const c = (v: number) =>
    Math.round(Math.min(255, Math.max(0, v)))
      .toString(16)
      .padStart(2, '0')
  return `#${c(r)}${c(g)}${c(b)}`
}

function channel(v: number) {
  const s = v / 255
  return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
}

export function luminance(hex: string) {
  const rgb = parseHex(hex)
  if (!rgb) throw new Error(`Not a hex color: ${hex}`)
  return 0.2126 * channel(rgb[0]) + 0.7152 * channel(rgb[1]) + 0.0722 * channel(rgb[2])
}

export function contrastRatio(a: string, b: string) {
  const la = luminance(a)
  const lb = luminance(b)
  const [hi, lo] = la > lb ? [la, lb] : [lb, la]
  return (hi + 0.05) / (lo + 0.05)
}

/** The candidate with the highest contrast against `bg`. */
export function bestContrast(bg: string, candidates: string[]) {
  let best = candidates[0] as string
  let bestRatio = -1
  for (const c of candidates) {
    const r = contrastRatio(bg, c)
    if (r > bestRatio) {
      best = c
      bestRatio = r
    }
  }
  return best
}

/** Linear mix of two hex colors in sRGB; `t` = weight of `b`. */
export function mix(a: string, b: string, t: number) {
  const x = parseHex(a)
  const y = parseHex(b)
  if (!x || !y) return a
  return toHex([x[0] + (y[0] - x[0]) * t, x[1] + (y[1] - x[1]) * t, x[2] + (y[2] - x[2]) * t])
}
