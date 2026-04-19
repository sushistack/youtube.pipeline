import { Outlet } from 'react-router'
import { useViewportCollapse } from '../../hooks/useViewportCollapse'
import { useUIStore } from '../../stores/useUIStore'
import { Sidebar } from './Sidebar'

export function AppShell() {
  const sidebar_collapsed = useUIStore((state) => state.sidebar_collapsed)
  const toggle_sidebar = useUIStore((state) => state.toggle_sidebar)
  const is_narrow_viewport = useViewportCollapse()
  const effective_collapsed = is_narrow_viewport || sidebar_collapsed

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
      <main className="app-shell__main" role="main">
        <div className="app-shell__main-inner">
          <Outlet />
        </div>
      </main>
    </div>
  )
}
