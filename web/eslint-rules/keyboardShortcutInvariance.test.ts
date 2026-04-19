import { ESLint } from 'eslint'
import tseslint from 'typescript-eslint'
import { describe, expect, it } from 'vitest'
import { keyboardShortcutInvarianceRule } from './keyboardShortcutInvariance.js'

async function lintText(source: string) {
  const eslint = new ESLint({
    ignore: false,
    overrideConfig: [
      {
        files: ['**/*.{ts,tsx}'],
        languageOptions: {
          parser: tseslint.parser,
          parserOptions: {
            ecmaFeatures: {
              jsx: true,
            },
            ecmaVersion: 'latest',
            sourceType: 'module',
          },
        },
        plugins: {
          local: {
            rules: {
              'keyboard-shortcut-invariance': keyboardShortcutInvarianceRule,
            },
          },
        },
        rules: {
          'local/keyboard-shortcut-invariance': 'error',
        },
      },
    ],
    overrideConfigFile: true,
  })

  const [result] = await eslint.lintText(source, {
    filePath: 'src/fixture.tsx',
  })

  return result
}

describe('keyboardShortcutInvarianceRule — shortcut registration objects', () => {
  it('passes when Enter maps to a primary action', async () => {
    const result = await lintText(`
      const shortcuts = [
        { key: 'enter', action: 'approve' },
        { key: 'enter', action: 'primary' },
      ]
      export default shortcuts
    `)

    expect(result.messages).toHaveLength(0)
    expect(result.errorCount).toBe(0)
  })

  it('fails the build when Enter maps to a secondary action', async () => {
    const result = await lintText(`
      const shortcuts = [{ key: 'enter', action: 'reject' }]
      export default shortcuts
    `)

    expect(result.messages).toHaveLength(1)
    expect(result.messages[0]?.severity).toBe(2)
    expect(result.messages[0]?.message).toMatch(/Enter must map to a primary\/approve action/i)
    expect(result.errorCount).toBe(1)
  })

  it('passes when Escape maps to a secondary action', async () => {
    const result = await lintText(`
      const shortcuts = [
        { key: 'escape', action: 'reject' },
        { key: 'esc', action: 'secondary' },
      ]
      export default shortcuts
    `)

    expect(result.messages).toHaveLength(0)
    expect(result.errorCount).toBe(0)
  })

  it('fails the build when Escape maps to a primary action', async () => {
    const result = await lintText(`
      const shortcuts = [{ key: 'escape', action: 'approve' }]
      export default shortcuts
    `)

    expect(result.messages).toHaveLength(1)
    expect(result.messages[0]?.severity).toBe(2)
    expect(result.messages[0]?.message).toMatch(/Escape must map to a secondary/i)
    expect(result.errorCount).toBe(1)
  })
})

describe('keyboardShortcutInvarianceRule — JSX onKeyDown handlers', () => {
  it('fails when onKeyDown maps Enter to a reject call', async () => {
    const result = await lintText(`
      export function Row() {
        const reject = () => {}
        return (
          <button
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                reject()
              }
            }}
          />
        )
      }
    `)

    expect(result.errorCount).toBe(1)
    expect(result.messages[0]?.message).toMatch(/Enter must map to a primary/i)
  })

  it('passes when onKeyDown maps Enter to an approve call', async () => {
    const result = await lintText(`
      export function Row() {
        const approve = () => {}
        return (
          <button
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                approve()
              }
            }}
          />
        )
      }
    `)

    expect(result.errorCount).toBe(0)
  })

  it('fails when onKeyDown maps Escape to a submit call', async () => {
    const result = await lintText(`
      export function Row() {
        const submit = () => {}
        return (
          <button
            onKeyDown={(event) => {
              if (event.key === 'Escape') submit()
            }}
          />
        )
      }
    `)

    expect(result.errorCount).toBe(1)
    expect(result.messages[0]?.message).toMatch(/Escape must map to a secondary/i)
  })

  it('passes when onKeyDown maps Escape to a dismiss call', async () => {
    const result = await lintText(`
      export function Row() {
        const dismiss = () => {}
        return (
          <button
            onKeyDown={(event) => {
              if (event.key === 'Escape') dismiss()
            }}
          />
        )
      }
    `)

    expect(result.errorCount).toBe(0)
  })

  it('detects ternary-form JSX handler violations', async () => {
    const result = await lintText(`
      export function Row() {
        const approve = () => {}
        const cancel = () => {}
        return (
          <button
            onKeyDown={(e) => (e.key === 'Enter' ? cancel() : approve())}
          />
        )
      }
    `)

    expect(result.errorCount).toBe(1)
    expect(result.messages[0]?.message).toMatch(/Enter must map to a primary/i)
  })

  it('detects short-circuit logical-and JSX handlers', async () => {
    const result = await lintText(`
      export function Row() {
        const reject = () => {}
        return (
          <button
            onKeyDown={(e) => { e.key === 'Enter' && reject() }}
          />
        )
      }
    `)

    expect(result.errorCount).toBe(1)
    expect(result.messages[0]?.message).toMatch(/Enter must map to a primary/i)
  })
})

describe('keyboardShortcutInvarianceRule — documented boundary', () => {
  it('does not false-positive on plain string mentions of Enter/Escape in comments or literals', async () => {
    const result = await lintText(`
      // Press Enter to approve, Escape to reject.
      const label = 'Enter to continue, Escape to go back'
      export default label
    `)

    expect(result.messages).toHaveLength(0)
  })

  it('does not false-positive on unrelated objects with the words primary/secondary', async () => {
    const result = await lintText(`
      const palette = { primary: '#000', secondary: '#fff' }
      export default palette
    `)

    expect(result.messages).toHaveLength(0)
  })

  it('does not inspect dynamically-composed key strings', async () => {
    // Documented boundary: computed/non-literal keys are out of scope.
    // The rule only enforces static-literal shortcut registrations and JSX key handlers.
    const result = await lintText(`
      const prefix = 'en'
      const shortcuts = [{ key: \`\${prefix}ter\`, action: 'reject' }]
      export default shortcuts
    `)

    expect(result.messages).toHaveLength(0)
  })
})
