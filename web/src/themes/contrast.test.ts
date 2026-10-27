import { describe, expect, it } from 'vitest'
import { BUILTIN_THEMES, compileTheme, contrastReport, MIN_CONTRAST, validateTheme } from './compile'
import { contrastRatio } from './contrast'

describe('shipped themes meet WCAG 2.2 AA contrast', () => {
  for (const theme of BUILTIN_THEMES) {
    for (const mode of ['light', 'dark'] as const) {
      describe(`${theme.id} / ${mode}`, () => {
        const vars = compileTheme(theme)[mode]
        for (const { label, ratio } of contrastReport(vars)) {
          it(`${label} ≥ ${MIN_CONTRAST}:1 (got ${ratio.toFixed(2)})`, () => {
            expect(ratio).toBeGreaterThanOrEqual(MIN_CONTRAST)
          })
        }
      })
    }
  }

  it('theme files pass the same schema validation as uploads', () => {
    for (const theme of BUILTIN_THEMES) {
      const { builtin: _b, ...json } = theme
      expect(validateTheme(json)).toEqual([])
    }
  })

  it('contrast math matches known values', () => {
    expect(contrastRatio('#000000', '#ffffff')).toBeCloseTo(21, 5)
    expect(contrastRatio('#ffffff', '#ffffff')).toBeCloseTo(1, 5)
    expect(contrastRatio('#777777', '#ffffff')).toBeCloseTo(4.48, 2)
  })
})
