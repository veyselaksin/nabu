import { useEffect, useId, useState, type FormEvent } from "react";
import { ApiError, listRulesets, toIPA, type G2PResult, type RulesetInfo } from "./api";

// TODO: replace with the exact reconstruction disclaimer from README.md.
// README.md did not exist when this page was scaffolded, so this is interim text.
const DISCLAIMER =
  "Nabu's pronunciations are scholarly reconstructions, not recordings. Nobody knows exactly how " +
  "these languages sounded. Each output follows one published reconstruction (named and cited " +
  "with every result), and other scholars may disagree with it. The rule sets shipped right now " +
  "are placeholders for testing and are not yet sourced from their references.";

const EXAMPLE = "ꜥnḫ";

export function App() {
  const ids = { text: useId(), ruleset: useId(), speakHint: useId() };
  const [rulesets, setRulesets] = useState<RulesetInfo[]>([]);
  const [ruleset, setRuleset] = useState("");
  const [text, setText] = useState(EXAMPLE);
  const [result, setResult] = useState<G2PResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    listRulesets()
      .then((list) => {
        if (cancelled) return;
        setRulesets(list);
        setRuleset((current) => current || list[0]?.id || "");
      })
      .catch((e: unknown) => !cancelled && setError(message(e)));
    return () => {
      cancelled = true;
    };
  }, []);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      setResult(await toIPA(text, ruleset));
    } catch (err) {
      setResult(null);
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }

  const selected = rulesets.find((r) => r.id === ruleset);

  return (
    <main>
      <aside className="disclaimer" role="note">
        <strong>Reconstruction disclaimer.</strong> {DISCLAIMER}
      </aside>

      <h1>Nabu</h1>
      <p className="lede">Convert a transliteration to IPA using a published pronunciation reconstruction.</p>

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
          <button type="submit" disabled={busy || !ruleset || text.trim() === ""}>
            {busy ? "Converting…" : "Convert to IPA"}
          </button>
          {/* Disabled buttons don't fire hover events, so the tooltip sits on a wrapper. */}
          <span className="tooltip" data-tooltip="Coming soon">
            <button type="button" disabled aria-describedby={ids.speakHint}>
              Speak
            </button>
          </span>
          <span id={ids.speakHint} className="visually-hidden">
            Speech synthesis is coming soon.
          </span>
        </div>
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
        {!result && !error && <p className="muted">Enter a transliteration and press Convert.</p>}
      </section>
    </main>
  );
}

function message(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  return e instanceof Error ? e.message : String(e);
}
