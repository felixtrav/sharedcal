// Applies the last-used theme before the app's JS loads, so the first paint
// has the right colors. Loaded synchronously from <head>; the CSP forbids
// inline scripts, so this lives in its own file. Shape: src/themes/runtime.ts.
;(function () {
  var root = document.documentElement
  var prefersDark = false
  try {
    prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches
  } catch {
    /* ignore */
  }
  var stored = null
  try {
    stored = JSON.parse(window.localStorage.getItem('sharedcal.theme.v1') || 'null')
  } catch {
    stored = null
  }
  var mode = stored && stored.mode ? stored.mode : 'system'
  var dark = mode === 'dark' || (mode === 'system' && prefersDark)
  root.classList.toggle('dark', dark)
  root.style.colorScheme = dark ? 'dark' : 'light'
  root.setAttribute('data-mode', mode)
  if (!stored) return
  var vars = dark ? stored.dark : stored.light
  if (!vars || typeof vars !== 'object') return
  if (stored.id) root.setAttribute('data-theme', String(stored.id))
  for (var key in vars) {
    if (Object.prototype.hasOwnProperty.call(vars, key) && key.indexOf('--') === 0) {
      var value = String(vars[key])
      // Values were validated when compiled; still refuse anything that could break out of a declaration.
      if (!/[;{}<>]/.test(value)) root.style.setProperty(key, value)
    }
  }
})()
