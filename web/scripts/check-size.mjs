#!/usr/bin/env node
// Fails when the JavaScript needed for the first page load exceeds the budget.
// "Initial" = the entry chunk plus everything it imports statically (recursively),
// as recorded in Vite's build manifest. Lazy routes and components are excluded.
import { readFileSync, existsSync } from 'node:fs'
import { join } from 'node:path'
import { gzipSync } from 'node:zlib'

const BUDGET_KB = Number(process.env.SIZE_BUDGET_KB ?? 300)
const dist = join(import.meta.dirname, '..', 'dist')
const manifestPath = join(dist, '.vite', 'manifest.json')

if (!existsSync(manifestPath)) {
  console.error('dist/.vite/manifest.json not found. Run `npm run build` first.')
  process.exit(2)
}

const manifest = JSON.parse(readFileSync(manifestPath, 'utf8'))
const entries = Object.entries(manifest).filter(([, c]) => c.isEntry)
const seen = new Set()
const visit = (key) => {
  if (seen.has(key)) return
  seen.add(key)
  for (const dep of manifest[key]?.imports ?? []) visit(dep)
}
for (const [key] of entries) visit(key)

let total = 0
const rows = []
for (const key of seen) {
  const file = manifest[key].file
  if (!file.endsWith('.js')) continue
  const gz = gzipSync(readFileSync(join(dist, file)), { level: 9 }).length
  total += gz
  rows.push([file, gz])
}
rows.sort((a, b) => b[1] - a[1])
for (const [file, gz] of rows) console.log(`${(gz / 1024).toFixed(1).padStart(8)} KB  ${file}`)
const kb = total / 1024
console.log(`${'-'.repeat(40)}\n${kb.toFixed(1).padStart(8)} KB  initial JS (gzip), budget ${BUDGET_KB} KB`)
if (kb > BUDGET_KB) {
  console.error(`✗ Initial JavaScript is ${(kb - BUDGET_KB).toFixed(1)} KB over budget.`)
  process.exit(1)
}
console.log('✓ Within budget')
