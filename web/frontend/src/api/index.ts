import axios from 'axios'

const api = axios.create({
  baseURL: '/admin',
  timeout: 10000,
})

export interface DecisionRecord {
  id: number
  request_id: string
  timestamp: number
  logical_model: string
  selected_tier: string
  selected_model: string
  assistant_count: number
  reason: string
  trace_dir: string
  request_time_ms: number
  status_code: number
  error_message: string
}

export interface DecisionQueryResult {
  items: DecisionRecord[]
  has_more: boolean
}

export interface DistributionTier {
  name: string
  count: number
  ratio: number
}

export interface DistributionResult {
  total: number
  tiers: DistributionTier[]
}

export interface ModelConfig {
  low_model: string
  medium_model: string
  medium_probability: number
  high_model: string
  high_probability: number
  high_rounds?: number
  medium_rounds?: number
  direct_model?: string
  direct_prompt_enabled?: boolean
  anti_repetition_prompt_enabled?: boolean
  image_prompt_enabled?: boolean
}

export interface Config {
  listen: {
    address: string
  }
  upstream: {
    base_url: string
    timeout?: number
  }
  models: Record<string, ModelConfig>
  log?: {
    file?: string
    level?: string
  }
  trace?: {
    max_records?: number
    max_body_size?: number
    directory?: string
  }
  sqlite: {
    path: string
    max_records: number
  }
  ignored_user_input_prefixes?: string[]
}

export const decisionApi = {
  list: (params?: {
    logical_model?: string
    tier?: string
    query?: string
    limit?: number
    before?: number
  }) => api.get<DecisionQueryResult>('/decisions', { params }),

  distribution: (params?: {
    logical_model?: string
    minutes?: number
  }) => api.get<DistributionResult>('/decisions/distribution', { params }),
}

export const configApi = {
  get: () => api.get<Config>('/config/form'),
  
  save: (config: Config) => api.put<Config>('/config/form', config),
}

export interface ModelStats {
  model: string
  total: number
  success: number
  failure: number
}

export interface StatsSnapshot {
  total: number
  success: number
  failure: number
  models: ModelStats[]
  logical_models: ModelStats[]
}

export const statsApi = {
  get: () => api.get<StatsSnapshot>('/stats/models'),
  
  reset: () => api.post('/stats/models/reset'),
}

export interface TraceDetail {
  trace_dir: string
  decision: Record<string, unknown> | null
  last_message: Record<string, unknown> | null
  headers: Record<string, string[]> | null
}

export const traceApi = {
  get: (traceDir: string) => api.get<TraceDetail>(`/traces/${encodeURIComponent(traceDir)}`),
  requestDownloadUrl: (traceDir: string) => `/admin/traces/${encodeURIComponent(traceDir)}/request`,
}
