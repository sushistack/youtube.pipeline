import type { Page, Route } from '@playwright/test'

// Mock helpers for the Go-served /api/* surface. Each spec composes the
// pieces it needs; unmatched requests fall through to the real backend
// (which serves a clean test DB at .tmp/playwright/pipeline.db).

export type RunStage =
  | 'pending'
  | 'research'
  | 'structure'
  | 'write'
  | 'visual_break'
  | 'review'
  | 'critic'
  | 'scenario_review'
  | 'character_pick'
  | 'image'
  | 'tts'
  | 'batch_review'
  | 'assemble'
  | 'metadata_ack'
  | 'complete'

export type RunStatus =
  | 'pending'
  | 'running'
  | 'waiting'
  | 'completed'
  | 'failed'
  | 'cancelled'

export interface RunFixture {
  id: string
  scp_id: string
  stage: RunStage
  status: RunStatus
  cost_usd?: number
  duration_ms?: number
  retry_reason?: string | null
  retry_count?: number
  frozen_descriptor?: string | null
  selected_character_id?: string | null
  character_query_key?: string | null
  human_override?: boolean
  critic_score?: number | null
  updated_at?: string
  created_at?: string
  token_in?: number
  token_out?: number
}

function fillRun(run: RunFixture) {
  const now = '2026-04-25T00:00:00Z'
  return {
    id: run.id,
    scp_id: run.scp_id,
    stage: run.stage,
    status: run.status,
    cost_usd: run.cost_usd ?? 0,
    duration_ms: run.duration_ms ?? 0,
    retry_count: run.retry_count ?? 0,
    retry_reason: run.retry_reason ?? null,
    frozen_descriptor: run.frozen_descriptor ?? null,
    selected_character_id: run.selected_character_id ?? null,
    character_query_key: run.character_query_key ?? null,
    human_override: run.human_override ?? false,
    critic_score: run.critic_score ?? null,
    updated_at: run.updated_at ?? now,
    created_at: run.created_at ?? now,
    token_in: run.token_in ?? 0,
    token_out: run.token_out ?? 0,
  }
}

function jsonOk(route: Route, body: unknown, headers: Record<string, string> = {}) {
  return route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(body),
    headers,
  })
}

function envelope<T>(data: T) {
  return { data, version: 1 }
}

export interface MockState {
  runs: RunFixture[]
  scenes?: Record<string, Array<{ scene_index: number; narration: string }>>
  reviewItems?: Record<string, ReviewItemFixture[]>
  characters?: Record<string, CharacterCandidateFixture[]>
  descriptorPrefill?: Record<string, { auto: string; prior: string | null }>
  settings?: SettingsFixture
  metadata?: Record<string, MetadataBundleFixture>
  manifest?: Record<string, SourceManifestFixture>
}

export interface MetadataBundleFixture {
  version?: number
  generated_at?: string
  run_id: string
  scp_id: string
  title: string
  ai_generated?: { narration: boolean; imagery: boolean; tts: boolean }
  models_used?: Record<string, { provider: string; model: string; voice?: string }>
}

export interface SourceManifestFixture {
  version?: number
  generated_at?: string
  run_id: string
  scp_id: string
  source_url: string
  author_name?: string
  license: string
  license_url?: string
  license_chain?: Array<{
    component: string
    source_url: string
    author_name: string
    license: string
  }>
}

function fillMetadata(bundle: MetadataBundleFixture) {
  return {
    version: bundle.version ?? 1,
    generated_at: bundle.generated_at ?? '2026-04-25T00:00:00Z',
    run_id: bundle.run_id,
    scp_id: bundle.scp_id,
    title: bundle.title,
    ai_generated: bundle.ai_generated ?? {
      narration: true,
      imagery: true,
      tts: true,
    },
    models_used: bundle.models_used ?? {
      'qwen-max': { provider: 'dashscope', model: 'qwen-max' },
      'sambert-zhichu-v1': {
        provider: 'dashscope',
        model: 'sambert-zhichu-v1',
        voice: 'zhichu',
      },
    },
  }
}

function fillManifest(manifest: SourceManifestFixture) {
  return {
    version: manifest.version ?? 1,
    generated_at: manifest.generated_at ?? '2026-04-25T00:00:00Z',
    run_id: manifest.run_id,
    scp_id: manifest.scp_id,
    source_url: manifest.source_url,
    author_name: manifest.author_name ?? 'Dr. Example',
    license: manifest.license,
    license_url: manifest.license_url ?? 'https://creativecommons.org/licenses/by-sa/3.0/',
    license_chain: manifest.license_chain ?? [
      {
        component: 'SCP article text',
        source_url: manifest.source_url,
        author_name: manifest.author_name ?? 'Dr. Example',
        license: manifest.license,
      },
    ],
  }
}

export interface ReviewItemFixture {
  scene_index: number
  narration: string
  review_status:
    | 'pending'
    | 'waiting_for_review'
    | 'auto_approved'
    | 'approved'
    | 'rejected'
  high_leverage?: boolean
  critic_score?: number | null
  retry_exhausted?: boolean
  regen_attempts?: number
  shots?: Array<{ duration_s: number; image_path: string; transition: string; visual_descriptor: string }>
}

function fillReviewItem(item: ReviewItemFixture) {
  return {
    scene_index: item.scene_index,
    narration: item.narration,
    review_status: item.review_status,
    high_leverage: item.high_leverage ?? false,
    critic_score: item.critic_score ?? null,
    critic_breakdown: null,
    content_flags: [],
    regen_attempts: item.regen_attempts ?? 0,
    retry_exhausted: item.retry_exhausted ?? false,
    shots: item.shots ?? [],
    clip_path: null,
    tts_path: null,
    tts_duration_ms: null,
    high_leverage_reason: null,
    high_leverage_reason_code: null,
    previous_version: null,
    prior_rejection: null,
  }
}

export interface CharacterCandidateFixture {
  id: string
  title: string
  image_url: string
  page_url: string
  preview_url?: string | null
  source_label?: string | null
}

export interface SettingsFixture {
  config: Record<string, string | number>
  etag?: string
}

function defaultSettings(): SettingsFixture {
  return {
    etag: 'W/"settings-v1"',
    config: {
      writer_provider: 'dashscope',
      writer_model: 'qwen-max',
      critic_provider: 'deepseek',
      critic_model: 'deepseek-v4-flash',
      image_provider: 'dashscope',
      image_model: 'wanx-v1',
      tts_provider: 'dashscope',
      tts_model: 'sambert-zhichu-v1',
      tts_voice: 'zhichu',
      tts_audio_format: 'mp3',
      cost_cap_research: 0.05,
      cost_cap_write: 0.1,
      cost_cap_image: 0.2,
      cost_cap_tts: 0.05,
      cost_cap_assemble: 0.05,
      cost_cap_per_run: 0.5,
    },
  }
}

export interface InstallApiMocksOptions {
  state: MockState
  spies?: ApiSpies
}

export interface ApiSpies {
  decisionRequests?: Array<{
    runId: string
    scene_index: number
    decision_type: string
    note?: string | null
  }>
  approveAllRequests?: Array<{ runId: string; focus_scene_index: number }>
  undoCount?: { value: number }
  resumeCount?: { value: number }
  ackCount?: { value: number }
  settingsPuts?: Array<{ config: Record<string, unknown>; ifMatch: string | null }>
  characterPicks?: Array<{ runId: string; candidate_id: string; frozen_descriptor: string }>
  narrationEdits?: Array<{ runId: string; scene_index: number; narration: string }>
}

export async function installApiMocks(
  page: Page,
  options: InstallApiMocksOptions,
) {
  const { state, spies } = options
  const settings = state.settings ?? defaultSettings()

  // GET /api/runs (list)
  await page.route('**/api/runs', async (route) => {
    if (route.request().method() === 'GET') {
      return jsonOk(
        route,
        envelope({
          items: state.runs.map(fillRun),
          total: state.runs.length,
        }),
      )
    }
    return route.fallback()
  })

  // GET /api/runs/:id  +  GET /api/runs/:id/status  +  POST /api/runs/:id/resume
  // +  GET /api/runs/:id/scenes  +  GET /api/runs/:id/review-items
  // +  POST /api/runs/:id/decisions  +  POST /api/runs/:id/approve-all-remaining
  // +  POST /api/runs/:id/undo  +  POST /api/runs/:id/scenes/:idx/edit
  // +  POST /api/runs/:id/scenes/:idx/regen  +  GET /api/runs/:id/characters[?q]
  // +  GET /api/runs/:id/characters/descriptor  +  POST /api/runs/:id/characters/pick
  // +  POST /api/runs/:id/metadata/ack
  await page.route(/.*\/api\/runs\/[^/]+(?:\/.*)?$/, async (route) => {
    const url = new URL(route.request().url())
    const method = route.request().method()
    const path = url.pathname.replace(/^.*\/api\/runs\//, '')
    const segments = path.split('/').filter(Boolean)
    const runId = decodeURIComponent(segments[0])
    const tail = segments.slice(1).join('/')
    const run = state.runs.find((r) => r.id === runId)

    if (!run) {
      return route.fulfill({
        status: 404,
        contentType: 'application/json',
        body: JSON.stringify({
          error: { code: 'NOT_FOUND', message: 'run not found' },
          version: 1,
        }),
      })
    }

    // /runs/:id  (detail)
    if (method === 'GET' && tail === '') {
      return jsonOk(route, envelope(fillRun(run)))
    }

    // /runs/:id/status
    if (method === 'GET' && tail === 'status') {
      return jsonOk(
        route,
        envelope({
          run: fillRun(run),
          decisions_summary: {
            approved_count: 0,
            pending_count: 0,
            rejected_count: 0,
          },
          changes_since_last_interaction: [],
        }),
      )
    }

    // /runs/:id/resume
    if (method === 'POST' && tail === 'resume') {
      if (spies?.resumeCount) spies.resumeCount.value += 1
      // Flip to running so the FailureBanner unmounts on refetch.
      run.status = 'running'
      return jsonOk(route, envelope(fillRun(run)))
    }

    // /runs/:id/scenes
    if (method === 'GET' && tail === 'scenes') {
      const scenes = state.scenes?.[runId] ?? []
      return jsonOk(route, envelope({ items: scenes, total: scenes.length }))
    }

    // /runs/:id/scenes/:idx/edit  or /scenes/:idx/regen
    const sceneEdit = tail.match(/^scenes\/(\d+)\/(edit|regen)$/)
    if (method === 'POST' && sceneEdit) {
      const sceneIndex = Number(sceneEdit[1])
      if (sceneEdit[2] === 'edit') {
        const body = (await route.request().postDataJSON()) as { narration: string }
        spies?.narrationEdits?.push({
          runId,
          scene_index: sceneIndex,
          narration: body.narration,
        })
        const list = state.scenes?.[runId]
        const target = list?.find((s) => s.scene_index === sceneIndex)
        if (target) target.narration = body.narration
        return jsonOk(
          route,
          envelope({ scene_index: sceneIndex, narration: body.narration }),
        )
      }
      // regen (used by UI-E2E-04 chord, not asserted in this batch directly)
      return jsonOk(
        route,
        envelope({
          scene_index: sceneIndex,
          regen_attempts: 1,
          retry_exhausted: false,
        }),
      )
    }

    // /runs/:id/review-items
    if (method === 'GET' && tail === 'review-items') {
      const items = state.reviewItems?.[runId] ?? []
      return jsonOk(
        route,
        envelope({
          items: items.map(fillReviewItem),
          total: items.length,
        }),
      )
    }

    // /runs/:id/decisions
    if (method === 'POST' && tail === 'decisions') {
      const body = (await route.request().postDataJSON()) as {
        scene_index: number
        decision_type: 'approve' | 'reject' | 'skip_and_remember'
        note?: string | null
      }
      spies?.decisionRequests?.push({
        runId,
        scene_index: body.scene_index,
        decision_type: body.decision_type,
        note: body.note ?? null,
      })
      const list = state.reviewItems?.[runId]
      const item = list?.find((i) => i.scene_index === body.scene_index)
      if (item) {
        if (body.decision_type === 'approve') item.review_status = 'approved'
        if (body.decision_type === 'reject') item.review_status = 'rejected'
      }
      const nextActionable = list?.find(
        (i) =>
          i.scene_index !== body.scene_index &&
          (i.review_status === 'waiting_for_review' ||
            i.review_status === 'pending'),
      )
      return jsonOk(
        route,
        envelope({
          decision_type: body.decision_type,
          scene_index: body.scene_index,
          next_scene_index:
            nextActionable?.scene_index ?? body.scene_index,
          regen_attempts: body.decision_type === 'reject' ? 1 : 0,
          retry_exhausted: false,
          prior_rejection: null,
        }),
      )
    }

    // /runs/:id/approve-all-remaining
    if (method === 'POST' && tail === 'approve-all-remaining') {
      const body = (await route.request().postDataJSON()) as {
        focus_scene_index: number
      }
      spies?.approveAllRequests?.push({
        runId,
        focus_scene_index: body.focus_scene_index,
      })
      const list = state.reviewItems?.[runId] ?? []
      const targets = list.filter(
        (i) =>
          i.review_status === 'waiting_for_review' ||
          i.review_status === 'pending',
      )
      const indices = targets.map((i) => i.scene_index)
      for (const item of targets) item.review_status = 'approved'
      const aggregateId =
        indices.length === 0 ? '' : `agg-${runId}-${Date.now()}`
      return jsonOk(
        route,
        envelope({
          aggregate_command_id: aggregateId,
          approved_count: indices.length,
          approved_scene_indices: indices,
          focus_scene_index: body.focus_scene_index,
        }),
      )
    }

    // /runs/:id/undo
    if (method === 'POST' && tail === 'undo') {
      if (spies?.undoCount) spies.undoCount.value += 1
      return jsonOk(
        route,
        envelope({
          undone_kind: 'approve',
          undone_scene_index: 0,
          focus_target: 'scene-card',
        }),
      )
    }

    // /runs/:id/characters  (with optional ?query=)
    if (method === 'GET' && tail === 'characters') {
      const candidates = state.characters?.[runId] ?? []
      return jsonOk(
        route,
        envelope({
          candidates,
          query: url.searchParams.get('query') ?? 'seeded',
          query_key: run.character_query_key ?? `qk-${runId}`,
        }),
      )
    }

    // /runs/:id/characters/descriptor
    if (method === 'GET' && tail === 'characters/descriptor') {
      const prefill = state.descriptorPrefill?.[runId] ?? {
        auto: 'auto-generated descriptor draft',
        prior: null,
      }
      return jsonOk(route, envelope(prefill))
    }

    // /runs/:id/characters/pick
    if (method === 'POST' && tail === 'characters/pick') {
      const body = (await route.request().postDataJSON()) as {
        candidate_id: string
        frozen_descriptor: string
      }
      spies?.characterPicks?.push({
        runId,
        candidate_id: body.candidate_id,
        frozen_descriptor: body.frozen_descriptor,
      })
      run.frozen_descriptor = body.frozen_descriptor
      run.selected_character_id = body.candidate_id
      run.stage = 'image'
      run.status = 'running'
      return jsonOk(route, envelope(fillRun(run)))
    }

    // /runs/:id/metadata  (raw JSON, no envelope — matches server shape)
    if (method === 'GET' && tail === 'metadata') {
      const bundle = state.metadata?.[runId]
      if (!bundle) {
        return route.fulfill({
          status: 404,
          contentType: 'application/json',
          body: JSON.stringify({
            error: { code: 'NOT_FOUND', message: 'metadata not found' },
            version: 1,
          }),
        })
      }
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(fillMetadata(bundle)),
      })
    }

    // /runs/:id/manifest  (raw JSON, no envelope)
    if (method === 'GET' && tail === 'manifest') {
      const m = state.manifest?.[runId]
      if (!m) {
        return route.fulfill({
          status: 404,
          contentType: 'application/json',
          body: JSON.stringify({
            error: { code: 'NOT_FOUND', message: 'manifest not found' },
            version: 1,
          }),
        })
      }
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(fillManifest(m)),
      })
    }

    // /runs/:id/metadata/ack — enforces the FR23 / NFR-L1 hard gate.
    // Mirrors the real handler's atomic stage/status guard
    // (RunStore.MarkComplete): only `metadata_ack + waiting` may transition
    // to `complete + completed`. Every other state returns 409 so Playwright
    // specs can observe the blocked-pre-ack regression guard (SMOKE-07 /
    // UI-E2E-06) without contract drift.
    if (method === 'POST' && tail === 'metadata/ack') {
      if (spies?.ackCount) spies.ackCount.value += 1
      if (run.stage !== 'metadata_ack' || run.status !== 'waiting') {
        return route.fulfill({
          status: 409,
          contentType: 'application/json',
          body: JSON.stringify({
            error: {
              code: 'CONFLICT',
              message: 'run is not awaiting metadata acknowledgment',
            },
            version: 1,
          }),
        })
      }
      run.stage = 'complete'
      run.status = 'completed'
      return jsonOk(route, envelope(fillRun(run)))
    }

    return route.fallback()
  })

  // /api/settings
  await page.route('**/api/settings', async (route) => {
    const method = route.request().method()
    if (method === 'GET') {
      return jsonOk(
        route,
        envelope({
          config: settings.config,
          env: {
            DASHSCOPE_API_KEY: { configured: true },
            DEEPSEEK_API_KEY: { configured: true },
          },
          budget: {
            source: { kind: 'none', label: 'No active run' },
            current_spend_usd: 0,
            soft_cap_usd: 0,
            hard_cap_usd: Number(settings.config.cost_cap_per_run ?? 0),
            progress_ratio: 0,
            status: 'safe',
          },
          application: { status: 'effective', effective_version: 1 },
        }),
        settings.etag ? { ETag: settings.etag } : {},
      )
    }
    if (method === 'PUT') {
      const ifMatch = route.request().headerValue('if-match')
      const body = (await route.request().postDataJSON()) as {
        config: Record<string, unknown>
      }
      spies?.settingsPuts?.push({
        config: body.config,
        ifMatch: (await ifMatch) ?? null,
      })
      // Persist the new config in the in-memory mock so a follow-up GET
      // reflects the saved value (UI-E2E-08 asserts this round-trip).
      settings.config = body.config as Record<string, string | number>
      const next = `W/"settings-${Date.now()}"`
      settings.etag = next
      return jsonOk(
        route,
        envelope({
          config: settings.config,
          env: {
            DASHSCOPE_API_KEY: { configured: true },
            DEEPSEEK_API_KEY: { configured: true },
          },
          budget: {
            source: { kind: 'none', label: 'No active run' },
            current_spend_usd: 0,
            soft_cap_usd: 0,
            hard_cap_usd: Number(settings.config.cost_cap_per_run ?? 0),
            progress_ratio: 0,
            status: 'safe',
          },
          application: { status: 'effective', effective_version: 2 },
        }),
        { ETag: next },
      )
    }
    return route.fallback()
  })

  // Tuning endpoints not exercised in this batch — return 404 fast so the
  // settings-page TimelineView does not stall on a slow real backend call.
  await page.route('**/api/decisions**', (route) =>
    jsonOk(
      route,
      envelope({
        items: [],
        next_cursor: null,
      }),
    ),
  )
}

export function makeSpies(): Required<ApiSpies> {
  return {
    decisionRequests: [],
    approveAllRequests: [],
    undoCount: { value: 0 },
    resumeCount: { value: 0 },
    ackCount: { value: 0 },
    settingsPuts: [],
    characterPicks: [],
    narrationEdits: [],
  }
}
