import { render, screen } from '@testing-library/react'
import '@testing-library/jest-dom'
import tokensCss from './tokens.css?raw'
import fontsCss from './fonts.css?raw'
import indexCss from '../index.css?raw'

const utilityFixtureCss = `
  .bg-background { background-color: rgb(var(--background)); }
  .bg-bg-subtle { background-color: rgb(var(--bg-subtle)); }
  .text-foreground { color: rgb(var(--foreground)); }
  .border-border { border: 1px solid rgb(var(--border)); }
`

const semanticTokens = {
  '--background': '15 17 23',
  '--bg-subtle': '19 21 29',
  '--bg-raised': '24 24 27',
  '--bg-input': '30 30 36',
  '--border-subtle': '34 34 40',
  '--border': '39 39 42',
  '--border-active': '63 63 70',
  '--foreground': '228 228 231',
  '--muted': '113 113 122',
  '--muted-subtle': '82 82 91',
  '--accent': '91 141 239',
  '--accent-hover': '123 164 244',
  '--accent-muted': '91 141 239 / 0.125',
  '--warning': '245 158 11',
  '--success': '34 197 94',
  '--destructive': '239 68 68',
} as const

const semanticTokenList = Object.keys(semanticTokens) as Array<
  keyof typeof semanticTokens
>

const spacingTokens = {
  '--space-1': '0.25rem',
  '--space-2': '0.5rem',
  '--space-3': '0.75rem',
  '--space-4': '1rem',
  '--space-6': '1.5rem',
  '--space-8': '2rem',
  '--space-12': '3rem',
} as const

const typeExpectations = [
  ['type-display', '1.875rem', '700'],
  ['type-h1', '1.5rem', '600'],
  ['type-h2', '1.25rem', '600'],
  ['type-h3', '1.125rem', '500'],
  ['type-body', '0.9375rem', '400'],
  ['type-body-sm', '0.875rem', '400'],
  ['type-caption', '0.75rem', '400'],
  ['type-mono', '0.875rem', '400'],
] as const

describe('global styling contract', () => {
  beforeAll(() => {
    const styleTag = document.createElement('style')
    styleTag.setAttribute('data-style-contract', 'true')
    styleTag.textContent = [tokensCss, fontsCss, utilityFixtureCss].join('\n')
    document.head.appendChild(styleTag)
  })

  beforeEach(() => {
    document.documentElement.removeAttribute('data-density')
    document.body.innerHTML = ''
  })

  it('defines the required semantic color tokens and spacing tokens on :root', () => {
    const rootStyles = getComputedStyle(document.documentElement)

    for (const [token, expected] of Object.entries(semanticTokens)) {
      expect(rootStyles.getPropertyValue(token).trim()).toBe(expected)
    }

    for (const [token, expected] of Object.entries(spacingTokens)) {
      expect(rootStyles.getPropertyValue(token).trim()).toBe(expected)
    }
  })

  it('returns non-empty computed values for all 16 semantic color custom properties', () => {
    const rootStyles = getComputedStyle(document.documentElement)

    for (const token of semanticTokenList) {
      expect(rootStyles.getPropertyValue(token).trim(), `${token} should be defined`).not.toBe('')
    }
  })

  it('switches density variables via the data-density attribute', () => {
    const root = document.documentElement
    const rootStyles = getComputedStyle(root)

    expect(rootStyles.getPropertyValue('--content-expand').trim()).toBe('1fr')
    expect(rootStyles.getPropertyValue('--preview-size').trim()).toBe('120px')
    expect(rootStyles.getPropertyValue('--motion-duration').trim()).toBe('150ms')

    root.setAttribute('data-density', 'elevated')

    const elevatedStyles = getComputedStyle(root)
    expect(elevatedStyles.getPropertyValue('--content-expand').trim()).toBe('2fr')
    expect(elevatedStyles.getPropertyValue('--preview-size').trim()).toBe('240px')
    expect(elevatedStyles.getPropertyValue('--motion-duration').trim()).toBe('250ms')
  })

  it('declares the required typography scale in rem with the expected weights', () => {
    const combinedCss = tokensCss + fontsCss
    for (const [className, expectedSize, expectedWeight] of typeExpectations) {
      const tokenName = className
      expect(combinedCss).toContain(`--${tokenName}-size: ${expectedSize};`)
      expect(combinedCss).toContain(`--${tokenName}-weight: ${expectedWeight};`)
      expect(fontsCss).toMatch(
        new RegExp(
          `\\.${className}\\s*\\{[^}]*font-size:\\s*var\\(--${tokenName}-size\\);`,
          's',
        ),
      )
    }
  })

  it('registers token-backed Tailwind-shaped utilities via the theme bridge', () => {
    render(
      <div
        data-testid="fixture-shell"
        className="bg-background text-foreground"
      >
        <div
          data-testid="fixture-card"
          className="bg-bg-subtle border-border"
        >
          token-backed utilities
        </div>
      </div>,
    )

    expect(screen.getByTestId('fixture-shell')).toBeInTheDocument()
    expect(screen.getByTestId('fixture-card')).toBeInTheDocument()
    expect(utilityFixtureCss).toContain('background-color: rgb(var(--background));')
    expect(utilityFixtureCss).toContain('border: 1px solid rgb(var(--border));')
    expect(indexCss).toContain('--color-background: rgb(var(--background));')
    expect(indexCss).toContain('--color-border: rgb(var(--border));')
  })

  it('keeps fonts self-hosted, uses swap, and avoids any light-mode branch', () => {
    const allCss = [fontsCss, tokensCss, indexCss].join('\n')

    expect(fontsCss).toContain('url("/assets/fonts/GeistVF.woff2")')
    expect(fontsCss).toContain('url("/assets/fonts/GeistMonoVF.woff2")')
    expect(fontsCss.match(/font-display:\s*swap/g)).toHaveLength(2)
    expect(fontsCss).not.toMatch(/https?:\/\//)
    expect(allCss).not.toMatch(/prefers-color-scheme:\s*light/)
    expect(allCss).not.toContain('dark:')
    expect(tokensCss).toContain('[data-density="elevated"]')
    expect(tokensCss).not.toContain('.density-elevated')
  })

  it('bridges Tailwind theme tokens from CSS variables instead of literal palette values', () => {
    expect(indexCss).toContain('--color-background: rgb(var(--background));')
    expect(indexCss).toContain('--font-sans: var(--font-sans-stack);')
    expect(indexCss).toContain('--spacing-4: var(--space-4);')
    expect(indexCss).not.toMatch(/#0f1117/i)
    expect(indexCss).not.toMatch(/#5b8def/i)
  })
})
