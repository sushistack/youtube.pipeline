import { ProductionShortcutPanel } from '../shared/ProductionShortcutPanel'

export function ProductionShell() {
  return (
    <section className="route-shell" aria-labelledby="production-shell-title">
      <p className="route-shell__eyebrow">Production workflow</p>
      <h1 id="production-shell-title" className="route-shell__title">
        Production
      </h1>
      <p className="route-shell__body">
        Review pipeline runs, inspect scenario output, and prepare assets for
        the next operator decision.
      </p>
      <ProductionShortcutPanel />
    </section>
  )
}
