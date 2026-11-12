export interface Evidence {
  event_seq: number
  kind: string
  quote: string
  verified: boolean
}

export interface Problem {
  statement: string
  symptoms?: string[] | null
  error_signatures?: string[] | null
  evidence?: number[] | null
}

export interface RootCause {
  text: string
  known: boolean
  confidence: string
  evidence?: number[] | null
}

export interface DiagnosisStep {
  order: number
  observation?: string
  hypothesis?: string
  action?: string
  command?: string
  command_kind?: string
  expected_signal?: string
  result?: string
  conclusion?: string
  evidence?: number[] | null
}

export interface EffectiveCommand {
  purpose: string
  command: string
  kind?: string
  expected_signal?: string
  risk?: string
  evidence?: number[] | null
}

export interface Concept {
  name: string
  stack?: string
  why_relevant?: string
  syntax?: string
  example?: string
  docs_url?: string
  evidence?: number[] | null
}

export interface RejectedHypothesis {
  hypothesis: string
  why_rejected: string
  evidence?: number[] | null
}

export interface BlindSpot {
  point: string
  kind?: string
  why_it_matters?: string
  evidence?: number[] | null
}

export interface SolutionStep {
  order: number
  action: string
  detail: string
  command?: string
  evidence?: number[] | null
}

export interface Insight {
  text: string
  confidence: string
  evidence?: number[] | null
}

export interface Pitfall {
  trap: string
  why: string
  avoid: string
  evidence?: number[] | null
}

export interface Verification {
  check: string
  expected: string
  observed: string
  status: string
  evidence?: number[] | null
}

export interface Artifacts {
  files?: string[] | null
  commands?: string[] | null
  urls?: string[] | null
}

export interface Uncertainty {
  field: string
  reason: string
  known: boolean
}

export interface Validation {
  evidence_checked: number
  evidence_verified: number
  warnings?: string[] | null
}

export interface Provenance {
  session_id: string
  log_file: string
  segment_id: string
  segment_hash: string
  event_range: [number, number]
  source_event_count: number
  provider: string
  model: string
  cli_version: string
  prompt_version: string
  prompt_hash: string
  redaction_hash: string
  schema_version: number
  segmenter_version: number
  redaction_version: number
  build_version: number
  cache_key?: string
  extracted_at: string
}

export interface Doc {
  schema_version: number
  id: string
  slug: string
  title: string
  summary: string
  project: string
  source: string
  knowledge_type: string
  occurred_at: string
  ended_at: string
  duration_minutes: number
  stack?: string[] | null
  tags?: string[] | null
  difficulty: string
  status: string
  review_status: string
  review_note?: string
  lifecycle: string
  stale_reason?: string
  staled_at?: string | null
  problem: Problem
  root_cause: RootCause
  diagnosis?: DiagnosisStep[] | null
  commands?: EffectiveCommand[] | null
  concepts?: Concept[] | null
  rejected_hypotheses?: RejectedHypothesis[] | null
  blind_spots?: BlindSpot[] | null
  learning_value?: string
  solution?: SolutionStep[] | null
  insights?: Insight[] | null
  pitfalls?: Pitfall[] | null
  verification?: Verification[] | null
  lessons?: string[] | null
  open_questions?: string[] | null
  artifacts: Artifacts
  evidence?: Evidence[] | null
  uncertain?: Uncertainty[] | null
  confidence: number
  validation: Validation
  provenance: Provenance
}

export interface SearchResult {
  id: string
  title: string
  project: string
  summary: string
  knowledge_type: string
  status: string
  review_status: string
  lifecycle?: string
  learning_value?: string
  tags: string[]
  stack: string[]
  confidence: number
  evidence_checked: number
  evidence_verified: number
  occurred_at: string
  markdown_path: string
  markdown_file?: string
  doc_path: string
  score: number
  matched?: string[]
}

export interface Project {
  name: string
  safe_name: string
  docs: number
  approved: number
  pending: number
  rejected: number
}

export interface StatusCache {
  dir: string
  exists: boolean
  size_bytes: number
  active_docs: number
  stale_docs: number
  pending: number
  approved: number
  rejected: number
}

export interface StatusQuestions {
  open: number
  resolved: number
}

export interface StatusCursor {
  path: string
  exists: boolean
  updated_at?: string
  files: number
  errors: number
  versions_match: boolean
}

export interface StatusOutputs {
  dir: string
  index_exists: boolean
  index_generated_at?: string
  index_stale: boolean
  digest_exists: boolean
  digest_modified_at?: string
  digest_stale: boolean
}

export interface StatusProvider {
  name: string
  bin: string
  bin_found: boolean
  dir: string
  dir_exists: boolean
  session_files: number
}

export interface StatusResponse {
  tool_version: string
  schema_version: number
  generated_at: string
  cache: StatusCache
  questions: StatusQuestions
  cursor: StatusCursor
  outputs: StatusOutputs
  providers: StatusProvider[]
  projects: Project[]
  knowledge_types: Record<string, number>
  recent: SearchResult[]
  warnings?: string[]
}

export interface DocListResponse {
  total: number
  docs: SearchResult[]
  warnings?: string[]
}

export interface RelatedDoc {
  id: string
  title: string
  score: number
  reasons: string[]
}

export interface QuestionEntry {
  id: string
  doc_id: string
  doc_title: string
  project: string
  question: string
  status: string
  note?: string
  resolved_at?: string
  occurred_at: string
}

export interface DocDetailResponse {
  doc: Doc
  markdown_path: string
  learn_markdown_path: string
  related: RelatedDoc[] | null
  questions: QuestionEntry[] | null
  warnings?: string[]
}

export interface ExecResult {
  known: boolean
  exit_code?: number | null
  timed_out: boolean
  canceled: boolean
  stdout?: string
  stderr?: string
}

export interface EventView {
  seq: number
  kind: string
  role?: string
  time?: string
  text?: string
  tool_name?: string
  tool_input?: unknown
  tool_input_text?: string
  tool_result?: string
  is_error?: boolean
  truncated?: boolean
  exec?: ExecResult
}

export interface EventsResponse {
  segment_id: string
  source: string
  session_id: string
  project: string
  title: string
  start: string
  end: string
  event_count: number
  events: EventView[]
}

export interface QuestionsResponse {
  entries: QuestionEntry[]
  open: number
  resolved: number
  warnings?: string[]
}

export interface ExportResponse {
  profile: string
  rendered: number
  stale_skipped: number
  pruned: number
  prune_skipped: number
  index_docs: number
  index_projects: number
  digest_path?: string
  digest_docs: number
  paths?: string[]
  warnings?: string[]
  out_dir: string
}

export interface ConfigResponse {
  tool_version: string
  cache_dir: string
  cursor_path: string
  out_dir: string
  addr: string
  local_only: boolean
  claude_bin: string
  codex_bin: string
  claude_dir: string
  codex_dir: string
}

export interface SelectableSegment {
  id: string
  title: string
  start: string
  end: string
  event_count: number
  user_text_count: number
  bytes: number
  extracted: boolean
}

export interface SelectableSession {
  selector: string
  source: string
  session_id: string
  project: string
  title: string
  start: string
  end: string
  event_count: number
  user_text_count: number
  bytes: number
  segment_count: number
  extracted: number
  segments: SelectableSegment[]
}

export interface SessionList {
  generated_at: string
  since?: string
  sessions: SelectableSession[]
  drops?: unknown[]
  warnings?: string[]
  segment_count: number
  drop_count: number
}

export interface ExtractJobRequest {
  provider: string
  since: string
  sessions: string[]
  segments?: string[]
  concurrency?: number
  timeout_seconds?: number
  model?: string
  include_subagents?: boolean
  full?: boolean
  force?: boolean
}

export interface JobEvent {
  seq: number
  phase: string
  total: number
  done: number
  segment_id?: string
  title?: string
  source?: string
  project?: string
  cached?: boolean
  failed?: boolean
  error?: string
  at: string
}

export interface JobSegment {
  segment_id: string
  title: string
  project: string
  source: string
  cached: boolean
  error?: string
  duration_ms: number
  confidence: number
  evidence_checked: number
  evidence_verified: number
}

export interface JobResult {
  files: number
  parsed: number
  skipped: number
  failed_files: number
  segments: number
  selected: number
  cache_hits: number
  extracted: number
  failed_segments: number
  reports: JobSegment[]
  warnings?: string[]
  duration_ms: number
}

export interface JobSnapshot {
  id: string
  status: 'running' | 'done' | 'failed' | 'canceled'
  started_at: string
  finished_at?: string
  total: number
  done: number
  cached: number
  extracted: number
  failed: number
  error?: string
  result?: JobResult
  events?: JobEvent[]
}
