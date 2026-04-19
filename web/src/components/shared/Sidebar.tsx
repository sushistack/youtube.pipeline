import { NavLink } from 'react-router'

interface SidebarProps {
  collapsed: boolean
  forced_collapsed: boolean
  on_toggle: () => void
}

const NAV_ITEMS = [
  { to: '/production', label: 'Production', icon: 'PR' },
  { to: '/tuning', label: 'Tuning', icon: 'TU' },
  { to: '/settings', label: 'Settings', icon: 'SE' },
] as const

export function Sidebar({
  collapsed,
  forced_collapsed,
  on_toggle,
}: SidebarProps) {
  const can_toggle = !forced_collapsed

  return (
    <aside
      className="sidebar"
      aria-label="Primary"
      data-collapsed={String(collapsed)}
    >
      <div className="sidebar__header">
        <div className="sidebar__brand" aria-label="youtube.pipeline">
          <span className="sidebar__brand-mark" aria-hidden="true">
            YP
          </span>
          <span className="sidebar__brand-label">youtube.pipeline</span>
        </div>
        <button
          type="button"
          className="sidebar__toggle"
          onClick={can_toggle ? on_toggle : undefined}
          disabled={!can_toggle}
          aria-disabled={!can_toggle}
          aria-expanded={!collapsed}
          aria-label={
            forced_collapsed
              ? 'Viewport is forcing the collapsed shell'
              : collapsed
                ? 'Expand sidebar'
                : 'Collapse sidebar'
          }
          title={
            forced_collapsed
              ? 'Viewport is forcing the collapsed shell'
              : collapsed
                ? 'Expand sidebar'
                : 'Collapse sidebar'
          }
        >
          <span aria-hidden="true">{collapsed ? '»' : '«'}</span>
        </button>
      </div>

      <nav className="sidebar__nav" aria-label="Workflow modes">
        {NAV_ITEMS.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end
            className={({ isActive }) =>
              `sidebar__link${isActive ? ' sidebar__link--active' : ''}`
            }
            aria-label={collapsed ? item.label : undefined}
            title={collapsed ? item.label : undefined}
          >
            <span className="sidebar__icon" aria-hidden="true">
              {item.icon}
            </span>
            <span className="sidebar__link-label">{item.label}</span>
          </NavLink>
        ))}
      </nav>
    </aside>
  )
}
