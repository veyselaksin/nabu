// Thin client for nabu-api. In dev, Vite proxies /v1 to the API; for other
// deployments set VITE_API_BASE (e.g. https://api.example.org) at build time.
//
// TODO(wasm): offer an in-browser path that runs the Go g2p package compiled
// with GOOS=js GOARCH=wasm (the package already builds for that target),
// falling back to the HTTP API. Load rule files from g2p/rules at build time.

const BASE = (import.meta.env.VITE_API_BASE as string | undefined) ?? "";

export interface RulesetInfo {
  id: string;
  language: string;
  name: string;
  reference: string;
  version: string;
}

export interface G2PResult {
  ipa: string;
  ruleset: string;
  reference: string;
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly details?: Record<string, unknown>,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response;
  try {
    res = await fetch(BASE + path, {
      ...init,
      headers: { "Content-Type": "application/json", ...init?.headers },
    });
  } catch {
    throw new ApiError(0, "network", "Could not reach the Nabu API. Is it running? (make run-api)");
  }
  const body: unknown = await res.json().catch(() => null);
  if (!res.ok) {
    const err = (body as { error?: { code?: string; message?: string; details?: Record<string, unknown> } } | null)
      ?.error;
    throw new ApiError(res.status, err?.code ?? "http_error", err?.message ?? `HTTP ${res.status}`, err?.details);
  }
  return body as T;
}

export async function listRulesets(): Promise<RulesetInfo[]> {
  const body = await request<{ rulesets: RulesetInfo[] }>("/v1/rulesets");
  return body.rulesets;
}

export function toIPA(text: string, ruleset: string): Promise<G2PResult> {
  return request<G2PResult>("/v1/g2p", { method: "POST", body: JSON.stringify({ text, ruleset }) });
}
