import type { AnalysisRun, BootstrapResponse, DigestResponse, Edition, RunResponse, SourceHealth } from "./types";

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, {
    ...init,
    headers: { Accept: "application/json", ...init?.headers },
  });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(payload.error || `请求失败 (${response.status})`);
  return payload as T;
}

export const api = {
  bootstrap: (profile: string) => request<BootstrapResponse>(`/api/bootstrap?profile=${encodeURIComponent(profile)}`),
  digest: (params: URLSearchParams) => request<DigestResponse>(`/api/digest?${params}`),
  health: () => request<SourceHealth[]>("/api/sources/health"),
  editions: (profile: string) => request<Edition[]>(`/api/editions?profile=${encodeURIComponent(profile)}`),
  runState: (profile: string) => request<AnalysisRun>(`/api/analysis-runs/current?profile=${encodeURIComponent(profile)}`),
  run: (profile: string) => request<RunResponse>(`/api/run?profile=${encodeURIComponent(profile)}`, { method: "POST" }),
  analyze: (profile: string, itemID: string) => request(`/api/analyze?profile=${encodeURIComponent(profile)}&item_id=${encodeURIComponent(itemID)}`, { method: "POST" }),
  feedback: (profile: string, itemID: string, action: string, remove: boolean) => remove
    ? request(`/api/feedback?${new URLSearchParams({ profile, item_id: itemID, action })}`, { method: "DELETE" })
    : request("/api/feedback", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ profile, item_id: itemID, action }) }),
  ingest: (body: { profile: string; url: string; title: string; content: string; tags: string[] }) => request<{ item_id: string }>("/api/ingest", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }),
};
