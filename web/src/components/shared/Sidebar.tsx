import { useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { NavLink } from 'react-router'
import { useLocation, useSearchParams } from 'react-router'
import { fetchRunList } from '../../lib/apiClient'
import { useKeyboardShortcuts } from '../../hooks/useKeyboardShortcuts'
import { compareRunsForInventory } from '../../lib/formatters'
import { formatShortcutHint } from '../../lib/keyboardShortcuts'
import { queryKeys } from '../../lib/queryKeys'
import { NewRunPanel } from '../production/NewRunPanel'
import { useNewRunCoordinator } from '../production/useNewRunCoordinator'
import { RunCard } from './RunCard'

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
  const query_client = useQueryClient()
  const [inventory_query, set_inventory_query] = useState('')
  const [search_params, set_search_params] = useSearchParams()
  const location = useLocation()
  const new_run_button_ref = useRef<HTMLButtonElement | null>(null)
  const {
    close_new_run_panel,
    is_open: new_run_open,
    open_new_run_panel,
    restore_focus,
  } = useNewRunCoordinator()
  const can_toggle = !forced_collapsed
  const selected_run_id = search_params.get('run')
  const is_production_route = location.pathname === '/production'
  const show_inventory = location.pathname === '/production' && !collapsed
  const runs_query = useQuery({
    queryFn: fetchRunList,
    queryKey: queryKeys.runs.list(),
    staleTime: 5_000,
  })
  const filtered_runs = (runs_query.data ?? [])
    .slice()
    .sort(compareRunsForInventory)
    .filter((run) => {
      if (inventory_query.trim().length === 0) {
        return true
      }

      const query = inventory_query.trim().toLowerCase()
      return (
        run.id.toLowerCase().includes(query) ||
        run.scp_id.toLowerCase().includes(query) ||
        run.stage.toLowerCase().includes(query) ||
        run.status.toLowerCase().includes(query)
      )
    })

  useKeyboardShortcuts(
    [
      {
        enabled: is_production_route,
        handler: () => {
          openNewRunPanel()
        },
        key: 'mod+n',
        prevent_default: true,
        scope: 'context',
      },
    ],
    { enabled: is_production_route },
  )

  function openNewRunPanel() {
    open_new_run_panel({
      restore_focus_to: new_run_button_ref.current,
    })
  }

  function handleNewRunSuccess(run_id: string) {
    set_search_params((current) => {
      const next = new URLSearchParams(current)
      next.set('run', run_id)
      return next
    })
    close_new_run_panel()
    window.requestAnimationFrame(() => {
      restore_focus()
    })
    void query_client.invalidateQueries({ queryKey: queryKeys.runs.list() })
  }

  return (
    <aside
      className="sidebar"
      aria-label="Primary"
      data-collapsed={String(collapsed)}
    >
      <div className="sidebar__header">
        <div className="sidebar__header-row">
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

        {is_production_route ? (
          <>
            <button
              ref={new_run_button_ref}
              type="button"
              className="sidebar__new-run-btn"
              aria-label="Create a new pipeline run"
              aria-expanded={new_run_open}
              onClick={openNewRunPanel}
              title={
                collapsed
                  ? `Create a new pipeline run (${formatShortcutHint('mod+n')})`
                  : undefined
              }
            >
              <span aria-hidden="true" className="sidebar__new-run-icon">
                +
              </span>
              <span className="sidebar__new-run-label">New Run</span>
              <span className="sidebar__new-run-hint">
                {formatShortcutHint('mod+n')}
              </span>
            </button>

            {new_run_open ? (
              <NewRunPanel
                on_cancel={() => {
                  close_new_run_panel()
                  window.requestAnimationFrame(() => {
                    restore_focus()
                  })
                }}
                on_success={(run) => {
                  handleNewRunSuccess(run.id)
                }}
              />
            ) : null}
          </>
        ) : null}
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

      {show_inventory ? (
        <section className="sidebar__inventory" aria-labelledby="sidebar-runs-title">
          <div className="sidebar__inventory-header">
            <p className="sidebar__inventory-eyebrow">Run inventory</p>
            <h2 id="sidebar-runs-title" className="sidebar__inventory-title">
              Active runs
            </h2>
          </div>

          <label className="sidebar__search">
            <span className="stage-stepper__sr-only">Search runs</span>
            <input
              type="search"
              className="sidebar__search-input"
              placeholder="Search runs"
              value={inventory_query}
              onChange={(event) => {
                set_inventory_query(event.target.value)
              }}
            />
          </label>

          <div className="sidebar__inventory-list">
            {filtered_runs.length > 0 ? (
              filtered_runs.map((run) => (
                <RunCard
                  key={run.id}
                  run={run}
                  selected={selected_run_id === run.id}
                  on_select={(run_id) => {
                    set_search_params((current) => {
                      const next = new URLSearchParams(current)
                      next.set('run', run_id)
                      return next
                    })
                  }}
                />
              ))
            ) : (
              <p className="sidebar__inventory-empty">
                {(runs_query.data ?? []).length === 0
                  ? 'No runs yet.'
                  : 'No runs match the current search.'}
              </p>
            )}
          </div>
        </section>
      ) : null}
    </aside>
  )
}
