import { useEffect, useId, useState, type FormEvent } from "react";
import {
  ApiError,
  listRulesets,
  speak,
  toIPA,
  voices,
  type G2PResult,
  type RulesetInfo,
  type SpeakResult,
  type VoicesInfo,
} from "./api";

// Mirrors the [!IMPORTANT] callout in README.md, plus what is specific to the voice.
const DISCLAIMER =
  "Nobody knows exactly how these languages sounded, and there are no recordings. Everything " +
  "Nabu outputs follows a published scholarly reconstruction, named and cited with every result; " +
  "other scholars reconstruct these languages differently. The voice was trained on recordings " +
  "of modern languages, so it reads the reconstructed IPA with a modern speaker's voice and accent. " +
  "This is an educational and research tool, not ground truth. The rule sets shipped right now " +
  "are placeholders for testing and are not yet sourced from their references.";

const EXAMPLE = "ꜥnḫ";

interface Spoken extends SpeakResult {
  url: string;
}

export function App() {
  const ids = { text: useId(), ruleset: useId(), speakHint: useId() };
  const [rulesets, setRulesets] = useState<RulesetInfo[]>([]);
  const [ruleset, setRuleset] = useState("");
  const [text, setText] = useState(EXAMPLE);
  const [result, setResult] = useState<G2PResult | null>(null);
  const [spoken, setSpoken] = useState<Spoken | null>(null);
  const [voice, setVoice] = useState<VoicesInfo | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<"convert" | "speak" | null>(null);

  useEffect(() => {
    let cancelled = false;
    listRulesets()
      .then((list) => {
        if (cancelled) return;
        setRulesets(list);
        setRuleset((current) => current || list[0]?.id || "");
      })
      .catch((e: unknown) => !cancelled && setError(message(e)));
    voices()
      .then((v) => !cancelled && setVoice(v))
      .catch(() => !cancelled && setVoice({ available: false, message: "Could not query /v1/voices." }));
    return () => {
      cancelled = true;
    };
  }, []);

  // Release the previous audio blob whenever a new one replaces it (and on unmount).
  useEffect(() => {
    return () => {
      if (spoken) URL.revokeObjectURL(spoken.url);
    };
  }, [spoken]);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setBusy("convert");
    setError(null);
    try {
      setResult(await toIPA(text, ruleset));
    } catch (err) {
      setResult(null);
      setError(message(err));
    } finally {
      setBusy(null);
    }
  }

  async function onSpeak() {
    setBusy("speak");
    setError(null);
    try {
      const r = await speak(text, ruleset);
      setSpoken({ ...r, url: URL.createObjectURL(r.audio) });
      setResult({ ipa: r.ipa, ruleset: r.ruleset, reference: r.reference });
    } catch (err) {
      setSpoken(null);
      setError(message(err));
    } finally {
      setBusy(null);
    }
  }

  const selected = rulesets.find((r) => r.id === ruleset);
  const canSubmit = busy === null && ruleset !== "" && text.trim() !== "";
  const speakAvailable = voice?.available === true;
  const speakHint = speakAvailable
    ? "Synthesize speech from the reconstructed IPA"
    : (voice?.message ?? "Checking whether speech synthesis is available…");

  return (
    <main>
      <aside className="disclaimer" role="note">
        <strong>Reconstruction disclaimer.</strong> {DISCLAIMER}
      </aside>

      <h1>Nabu</h1>
      <p className="lede">
        Convert a transliteration to IPA using a published pronunciation reconstruction, and hear it spoken.
      </p>

      <form onSubmit={onSubmit}>
        <label htmlFor={ids.text}>Transliteration</label>
        <textarea
          id={ids.text}
          value={text}
          onChange={(e) => setText(e.target.value)}
          rows={3}
          spellCheck={false}
          autoCapitalize="off"
          placeholder={EXAMPLE}
        />

        <label htmlFor={ids.ruleset}>Reconstruction</label>
        <select
          id={ids.ruleset}
          value={ruleset}
          onChange={(e) => setRuleset(e.target.value)}
          disabled={rulesets.length === 0}
        >
          {rulesets.length === 0 && <option value="">Loading…</option>}
          {rulesets.map((r) => (
            <option key={r.id} value={r.id}>
              {r.name} ({r.id} v{r.version})
            </option>
          ))}
        </select>
        {selected && <p className="reference">Source: {selected.reference}</p>}

        <div className="actions">
          <button type="submit" disabled={!canSubmit}>
            {busy === "convert" ? "Converting…" : "Convert to IPA"}
          </button>
          {/* Disabled buttons don't fire hover events, so the tooltip sits on a wrapper. */}
          <span className="tooltip" data-tooltip={speakAvailable ? undefined : "Speech unavailable"}>
            <button
              type="button"
              onClick={onSpeak}
              disabled={!canSubmit || !speakAvailable}
              aria-describedby={ids.speakHint}
            >
              {busy === "speak" ? "Speaking…" : "Speak"}
            </button>
          </span>
          <span id={ids.speakHint} className="visually-hidden">
            {speakHint}
          </span>
        </div>
        {voice && !speakAvailable && <p className="muted small">{speakHint}</p>}
      </form>

      <section aria-live="polite" className="output">
        <h2>IPA</h2>
        {error && <p className="error">{error}</p>}
        {result && !error && (
          <>
            <output className="ipa">{result.ipa === "" ? "∅" : `/${result.ipa}/`}</output>
            <p className="reference">
              {result.ruleset}: {result.reference}
            </p>
          </>
        )}
        {spoken && !error && (
          <figure className="player">
            <audio controls autoPlay src={spoken.url}>
              Your browser cannot play WAV audio.
            </audio>
            <figcaption>
              <span className="ipa small-ipa">/{spoken.ipa}/</span>
              <span className="reference">
                Reconstruction: {spoken.ruleset} v{spoken.rulesetVersion}: {spoken.reference}
              </span>
              <span className="reference">
                Voice: {spoken.model}, speaker {spoken.speaker}. Modern-language voice reading reconstructed IPA,
                not a recording.
              </span>
            </figcaption>
          </figure>
        )}
        {!result && !error && <p className="muted">Enter a transliteration and press Convert or Speak.</p>}
      </section>
    </main>
  );
}

function message(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  return e instanceof Error ? e.message : String(e);
}
