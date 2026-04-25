import type { ReactNode } from 'react'

interface ProductionMasterDetailProps {
  detail: ReactNode
  master?: ReactNode
  master_label?: string
  master_empty_message?: string
}

export function ProductionMasterDetail({
  detail,
  master,
  master_label = 'Scenes',
  master_empty_message = 'Scenes will appear once Phase A finishes.',
}: ProductionMasterDetailProps) {
  return (
    <div className="production-master-detail" data-has-master={String(Boolean(master))}>
      <aside
        className="production-master-detail__master"
        aria-label={master_label}
      >
        {master ?? (
          <div
            className="production-master-detail__master-empty"
            role="status"
            aria-live="polite"
          >
            {master_empty_message}
          </div>
        )}
      </aside>
      <section className="production-master-detail__detail" aria-label="Detail">
        {detail}
      </section>
    </div>
  )
}
