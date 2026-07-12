export type ViewMode = "all" | "recommended" | "delivered";
export type CollectionFilter = "all" | "saved" | "later";

export interface Feed {
  name: string;
  url: string;
  tags: string[];
  disabled: boolean;
}

export interface BootstrapResponse {
  profile: string;
  profiles: string[];
  feeds: Feed[];
}

export interface DigestItem {
  id: string;
  feed_name: string;
  feed_url: string;
  title: string;
  link: string;
  author: string;
  published_at: string;
  source_summary: string;
  content: string;
  model_label: string;
  model_name: string;
  score: number;
  should_push: boolean;
  analysis_title: string;
  summary: string;
  why: string;
  key_points: string[];
  tags: string[];
  analyzed_at: string;
  seen: boolean;
  pushed: boolean;
  feedback: string[];
  analysis_status: "pending" | "running" | "retry_wait" | "completed" | "failed" | string;
}

export interface DigestResponse {
  profile: string;
  items: DigestItem[];
  next_cursor?: string;
  total: number;
}

export interface SourceHealth {
  url: string;
  status: number;
  state: "healthy" | "rate_limited" | "error" | "unknown";
  last_error: string;
  fail_count: number;
  last_fetched_at: string;
  next_retry_at: string;
}

export interface Edition {
  id: number;
  slot: string;
  item_ids: string[];
  success: boolean;
  created_at: string;
}

export interface AnalysisRun {
  run_id?: number;
  profile: string;
  status: "initial" | "background" | "rate_limited" | "completed" | "partial_failed" | "idle" | string;
  total?: number;
  cached?: number;
  analyzed?: number;
  pending?: number;
  running?: number;
  retrying?: number;
  rate_limited?: number;
  failed?: number;
  created_at?: string;
  completed_at?: string;
}

export interface RunResponse {
  run_id: number;
  profile: string;
  fetched: number;
  candidate: number;
  analyzed: number;
  pushed: number;
  cached: number;
  queued: number;
  rate_limited: number;
  errors: string[];
}
