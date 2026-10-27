import js from '@eslint/js'
import jsxA11y from 'eslint-plugin-jsx-a11y'
import reactHooks from 'eslint-plugin-react-hooks'
import globals from 'globals'
import tseslint from 'typescript-eslint'

export default tseslint.config(
  { ignores: ['dist', 'node_modules', 'playwright-report', 'test-results', 'src/api/schema.d.ts'] },
  {
    files: ['**/*.{ts,tsx}'],
    extends: [js.configs.recommended, ...tseslint.configs.recommended, jsxA11y.flatConfigs.recommended],
    languageOptions: {
      ecmaVersion: 2023,
      globals: { ...globals.browser },
    },
    plugins: { 'react-hooks': reactHooks },
    rules: {
      ...reactHooks.configs.recommended.rules,
      '@typescript-eslint/no-explicit-any': 'error',
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_', varsIgnorePattern: '^_' }],
      'no-restricted-syntax': [
        'error',
        {
          selector: "JSXAttribute[name.name='dangerouslySetInnerHTML']",
          message: 'Use the sanitized <Markdown> component instead.',
        },
      ],
    },
  },
  {
    // The single sanctioned place that renders sanitized HTML.
    files: ['src/components/Markdown.tsx'],
    rules: { 'no-restricted-syntax': 'off' },
  },
  {
    files: ['e2e/**/*.ts', '*.config.ts', 'scripts/**/*.mjs', 'public/**/*.js'],
    languageOptions: { globals: { ...globals.node, ...globals.browser } },
  },
  {
    files: ['scripts/**/*.mjs', 'public/**/*.js'],
    extends: [js.configs.recommended],
    languageOptions: { globals: { ...globals.node, ...globals.browser } },
  },
)
