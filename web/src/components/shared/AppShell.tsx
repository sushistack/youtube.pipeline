import { useCallback, useRef } from 'react'
import { Outlet } from 'react-router'
import { useViewportCollapse } from '../../hooks/useViewportCollapse'
import { useUIStore } from '../../stores/useUIStore'
import { OnboardingModal } from './OnboardingModal'
import { Sidebar } from './Sidebar'

export function AppShell() {
  const main_ref = useRef<HTMLElement | null>(null)
  const onboarding_dismissed = useUIStore((state) => state.onboarding_dismissed)
  const dismiss_onboarding = useUIStore((state) => state.dismiss_onboarding)
  const sidebar_collapsed = useUIStore((state) => state.sidebar_collapsed)
  const toggle_sidebar = useUIStore((state) => state.toggle_sidebar)
  const is_narrow_viewport = useViewportCollapse()
  const effective_collapsed = is_narrow_viewport || sidebar_collapsed
  const handle_dismiss_onboarding = useCallback(() => {
    dismiss_onboarding()
    window.requestAnimationFrame(() => {
      main_ref.current?.focus()
    })
  }, [dismiss_onboarding])

  return (
    <div
      className="app-shell"
      data-testid="app-shell"
      data-collapsed={String(effective_collapsed)}
      data-forced-collapsed={String(is_narrow_viewport)}
      data-sidebar="shell"
    >
      <Sidebar
        collapsed={effective_collapsed}
        forced_collapsed={is_narrow_viewport}
        on_toggle={toggle_sidebar}
      />
      <main className="app-shell__main" ref={main_ref} role="main" tabIndex={-1}>
        <div className="app-shell__main-inner">
          <Outlet />
        </div>
      </main>
      {onboarding_dismissed ? null : (
        <OnboardingModal on_dismiss={handle_dismiss_onboarding} />
      )}
    </div>
  )
}
