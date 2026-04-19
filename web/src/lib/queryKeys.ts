export const queryKeys = {
  runs: {
    all: ['runs'] as const,
    characters: (run_id: string) => ['runs', 'characters', run_id] as const,
    descriptor: (run_id: string) => ['runs', 'descriptor', run_id] as const,
    detail: (run_id: string) => ['runs', 'detail', run_id] as const,
    list: () => ['runs', 'list'] as const,
    scenes: (run_id: string) => ['runs', 'scenes', run_id] as const,
    status: (run_id: string) => ['runs', 'status', run_id] as const,
    statusNone: ['runs', 'status', '__none__'] as const,
  },
}
