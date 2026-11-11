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

export interface DataSource {
  id: string;
  name: string;
  url: string;
  license: string;
  attribution?: string;
}

export interface Sign {
  value: string;
  sign: string;
  codepoint: string;
  gardiner?: string;
}

export interface RenderedWord {
  translit: string;
  hieroglyphs: string;
  /** "lexicon": attested spelling from a corpus; "phonetic": spelled sign by sign, not attested. */
  method: "lexicon" | "phonetic";
  attestation?: { hieroglyphs: string; count: number; total: number; variants: number };
  signs?: Sign[];
}

export interface RenderResult {
  input: string;
  hieroglyphs: string;
  words: RenderedWord[];
  sources: DataSource[];
  /** Whether the server has an attested-spelling lexicon loaded. */
  lexicon: boolean;
}

export function render(text: string): Promise<RenderResult> {
  return request<RenderResult>("/v1/render", { method: "POST", body: JSON.stringify({ text }) });
}

export interface VoicesInfo {
  available: boolean;
  message?: string;
  model?: {
    name: string;
    sample_rate: number;
    languages: string[];
    speakers: string[];
    min_speed: number;
    max_speed: number;
  };
}

export function voices(): Promise<VoicesInfo> {
  return request<VoicesInfo>("/v1/voices");
}

export interface SpeakResult {
  audio: Blob;
  ipa: string;
  ruleset: string;
  rulesetVersion: string;
  reference: string;
  model: string;
  speaker: number;
  /** Empty if some word has no hieroglyph sign; use render() for details. */
  hieroglyphs: string;
}

// The API percent-encodes header values that may contain non-ASCII text.
function header(res: Response, name: string): string {
  const raw = res.headers.get(name) ?? "";
  try {
    return decodeURIComponent(raw);
  } catch {
    return raw;
  }
}

export async function speak(
  text: string,
  ruleset: string,
  opts: { speaker?: number; speed?: number } = {},
): Promise<SpeakResult> {
  let res: Response;
  try {
    res = await fetch(BASE + "/v1/speak", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text, ruleset, ...opts }),
    });
  } catch {
    throw new ApiError(0, "network", "Could not reach the Nabu API. Is it running? (make run-api)");
  }
  if (!res.ok) {
    const body = (await res.json().catch(() => null)) as {
      error?: { code?: string; message?: string; details?: Record<string, unknown> };
    } | null;
    const err = body?.error;
    throw new ApiError(res.status, err?.code ?? "http_error", err?.message ?? `HTTP ${res.status}`, err?.details);
  }
  return {
    audio: await res.blob(),
    ipa: header(res, "X-Nabu-IPA"),
    ruleset: header(res, "X-Nabu-Ruleset"),
    rulesetVersion: header(res, "X-Nabu-Ruleset-Version"),
    reference: header(res, "X-Nabu-Reference"),
    model: header(res, "X-Nabu-Model"),
    speaker: Number(res.headers.get("X-Nabu-Speaker") ?? 0),
    hieroglyphs: header(res, "X-Nabu-Hieroglyphs"),
  };
}

export interface TranslatorInfo {
  available: boolean;
  message?: string;
  model?: string;
}

export function translator(): Promise<TranslatorInfo> {
  return request<TranslatorInfo>("/v1/translator");
}

export interface TranslationExample {
  transliteration: string;
  /** The TLA editors' German translation. */
  translation: string;
  date_not_before?: string;
  date_not_after?: string;
  score: number;
}

export interface TranslateResult {
  input: string;
  transliteration: string;
  /** Each output word and whether that form occurs in the corpus. */
  words: { form: string; attested: boolean; count: number }[];
  /** Share of output words attested in the corpus: a vocabulary check, not accuracy. */
  confidence: number;
  notes?: string;
  /** German search terms used to retrieve the examples. */
  keywords: string[];
  examples: TranslationExample[];
  model: string;
  sources: DataSource[];
}

export function translate(text: string): Promise<TranslateResult> {
  return request<TranslateResult>("/v1/translate", { method: "POST", body: JSON.stringify({ text, lang: "egy" }) });
}
