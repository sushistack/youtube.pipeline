export const queryKeys = {
  decisions: {
    timeline: (params: {
      before_created_at?: string
      before_id?: number
      decision_type?: string
      limit?: number
    }) => ['decisions', 'timeline', params] as const,
  },
  settings: {
    detail: () => ['settings', 'detail'] as const,
  },
  runs: {
    all: ['runs'] as const,
    characters: (run_id: string) => ['runs', 'characters', run_id] as const,
    descriptor: (run_id: string) => ['runs', 'descriptor', run_id] as const,
    detail: (run_id: string) => ['runs', 'detail', run_id] as const,
    list: () => ['runs', 'list'] as const,
    regenState: (run_id: string) => ['runs', 'regen-state', run_id] as const,
    reviewItems: (run_id: string) => ['runs', 'review-items', run_id] as const,
    scenes: (run_id: string) => ['runs', 'scenes', run_id] as const,
    status: (run_id: string) => ['runs', 'status', run_id] as const,
    statusNone: ['runs', 'status', '__none__'] as const,
    metadata: (run_id: string) => ['runs', 'metadata', run_id] as const,
    manifest: (run_id: string) => ['runs', 'manifest', run_id] as const,
  },
}
