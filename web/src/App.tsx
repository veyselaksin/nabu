import { useEffect, useId, useState, type FormEvent } from "react";
import {
  ApiError,
  listRulesets,
  render,
  speak,
  toIPA,
  translate,
  translator,
  voices,
  type G2PResult,
  type RenderResult,
  type RenderedWord,
  type RulesetInfo,
  type SpeakResult,
  type TranslateResult,
  type TranslatorInfo,
  type VoicesInfo,
} from "./api";

// Mirrors the [!IMPORTANT] callout in README.md, plus what is specific to the voice.
const DISCLAIMER =
  "Translations come from a language model guided by example sentences from a corpus; they are " +
  "unchecked machine output, and modern concepts have no ancient equivalent. " +
  "Nobody knows exactly how these languages sounded, and there are no recordings. Everything " +
  "Nabu outputs follows a published scholarly reconstruction, named and cited with every result; " +
  "other scholars reconstruct these languages differently. The voice was trained on recordings " +
  "of modern languages, so it reads the reconstructed IPA with a modern speaker's voice and accent. " +
  "Hieroglyphs are an attested spelling only where marked; otherwise they spell the word sign by sign, " +
  "which is not how Egyptian scribes wrote. " +
  "This is an educational and research tool, not ground truth. The rule sets shipped right now " +
  "are placeholders for testing and are not yet sourced from their references.";

const EXAMPLE = "ꜥnḫ";

interface Spoken extends SpeakResult {
  url: string;
}

export function App() {
  const ids = { english: useId(), translateHint: useId(), text: useId(), ruleset: useId(), speakHint: useId() };
  const [english, setEnglish] = useState("");
  const [translatorInfo, setTranslatorInfo] = useState<TranslatorInfo | null>(null);
  const [translation, setTranslation] = useState<TranslateResult | null>(null);
  const [translateError, setTranslateError] = useState<string | null>(null);
  const [rulesets, setRulesets] = useState<RulesetInfo[]>([]);
  const [ruleset, setRuleset] = useState("");
  const [text, setText] = useState(EXAMPLE);
  const [result, setResult] = useState<G2PResult | null>(null);
  const [spoken, setSpoken] = useState<Spoken | null>(null);
  const [voice, setVoice] = useState<VoicesInfo | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [glyphs, setGlyphs] = useState<RenderResult | null>(null);
  const [glyphError, setGlyphError] = useState<string | null>(null);
  const [busy, setBusy] = useState<"translate" | "convert" | "speak" | null>(null);

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
    translator()
      .then((t) => !cancelled && setTranslatorInfo(t))
      .catch(() => !cancelled && setTranslatorInfo({ available: false, message: "Could not query /v1/translator." }));
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

  // Script rendering is independent of G2P: a word without a sign must not
  // hide the IPA, and vice versa, so its errors are shown separately.
  async function refreshGlyphs(input: string) {
    try {
      setGlyphs(await render(input));
      setGlyphError(null);
    } catch (err) {
      setGlyphs(null);
      setGlyphError(message(err));
    }
  }

  async function convert(input: string) {
    setBusy("convert");
    setError(null);
    const glyphsDone = refreshGlyphs(input);
    try {
      setResult(await toIPA(input, ruleset));
    } catch (err) {
      setResult(null);
      setError(message(err));
    } finally {
      await glyphsDone;
      setBusy(null);
    }
  }

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    void convert(text);
  }

  // A translation replaces the transliteration and is converted right away.
  async function onTranslate(e: FormEvent) {
    e.preventDefault();
    setBusy("translate");
    setTranslateError(null);
    let res: TranslateResult;
    try {
      res = await translate(english);
    } catch (err) {
      setTranslation(null);
      setTranslateError(message(err));
      setBusy(null);
      return;
    }
    setTranslation(res);
    setText(res.transliteration);
    setSpoken(null);
    if (ruleset !== "") {
      await convert(res.transliteration);
    } else {
      setBusy(null);
    }
  }

  async function onSpeak() {
    setBusy("speak");
    setError(null);
    const glyphsDone = refreshGlyphs(text);
    try {
      const r = await speak(text, ruleset);
      setSpoken({ ...r, url: URL.createObjectURL(r.audio) });
      setResult({ ipa: r.ipa, ruleset: r.ruleset, reference: r.reference });
    } catch (err) {
      setSpoken(null);
      setError(message(err));
    } finally {
      await glyphsDone;
      setBusy(null);
    }
  }

  const selected = rulesets.find((r) => r.id === ruleset);
  const canSubmit = busy === null && ruleset !== "" && text.trim() !== "";
  const speakAvailable = voice?.available === true;
  const translateAvailable = translatorInfo?.available === true;
  const canTranslate = busy === null && translateAvailable && english.trim() !== "";
  const translateHint = translateAvailable
    ? `Translate with ${translatorInfo?.model || "the configured model"}; on CPU this can take a minute or more`
    : (translatorInfo?.message ?? "Checking whether translation is available…");
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
        Translate English into Middle Egyptian, render the transliteration in hieroglyphs, convert it to IPA using
        a published pronunciation reconstruction, and hear it spoken.
      </p>

      <form onSubmit={onTranslate}>
        <label htmlFor={ids.english}>English</label>
        <textarea
          id={ids.english}
          value={english}
          onChange={(e) => setEnglish(e.target.value)}
          rows={2}
          placeholder="May the king live"
        />
        <div className="actions">
          <span className="tooltip" data-tooltip={translateAvailable ? undefined : "Translation unavailable"}>
            <button type="submit" disabled={!canTranslate} aria-describedby={ids.translateHint}>
              {busy === "translate" ? "Translating…" : "Translate"}
            </button>
          </span>
          <span id={ids.translateHint} className="visually-hidden">
            {translateHint}
          </span>
        </div>
        {translatorInfo && !translateAvailable && <p className="muted small">{translateHint}</p>}
        {busy === "translate" && <p className="muted small">{translateHint}.</p>}
        {translateError && <p className="error">{translateError}</p>}
        {translation && !translateError && <Translation result={translation} />}
      </form>

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
        <h2>Hieroglyphs</h2>
        {glyphError && <p className="error">{glyphError}</p>}
        {glyphs && !glyphError && <Hieroglyphs result={glyphs} />}
        {!glyphs && !glyphError && <p className="muted">Press Convert or Speak to render the text.</p>}
      </section>

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

function Translation({ result }: { result: TranslateResult }) {
  const attested = result.words.filter((w) => w.attested).length;
  return (
    <div className="translation">
      <p className="translit" lang="egy-Latn">
        {result.words.map((w, i) => (
          <span
            key={i}
            className={`translit-word ${w.attested ? "attested" : "unattested"}`}
            title={w.attested ? `${w.form}: ${w.count}× in the corpus` : `${w.form}: not found in the corpus`}
          >
            {w.form}
          </span>
        ))}
      </p>
      <p className="small">
        <strong>Vocabulary check:</strong> {attested} of {result.words.length} words occur in the corpus
        {attested < result.words.length ? " (dashed: not found, possibly invented by the model)" : ""}. This checks
        vocabulary only, not grammar or meaning.
      </p>
      {result.notes && <p className="small muted">Model note: {result.notes}</p>}
      <details className="small">
        <summary>
          {result.examples.length} example sentence{result.examples.length === 1 ? "" : "s"} given to the model
          {result.keywords.length > 0 ? ` (searched for: ${result.keywords.join(", ")})` : ""}
        </summary>
        <ol className="examples">
          {result.examples.map((ex, i) => (
            <li key={i}>
              <span lang="egy-Latn">{ex.transliteration}</span>
              <br />
              <span className="muted" lang="de">
                {ex.translation}
              </span>
            </li>
          ))}
        </ol>
        <p className="muted">The corpus translations are German, as published by the TLA.</p>
      </details>
      <p className="reference">Model: {result.model}.</p>
      {result.sources.map((s) => (
        <p key={s.id} className="reference">
          Examples and vocabulary: <a href={s.url}>{s.name}</a> ({s.license}){s.attribution ? `. ${s.attribution}` : ""}
        </p>
      ))}
    </div>
  );
}

function Hieroglyphs({ result }: { result: RenderResult }) {
  const phonetic = result.words.some((w) => w.method === "phonetic" && w.hieroglyphs !== "");
  const attested = result.words.some((w) => w.method === "lexicon");
  return (
    <>
      <p className="glyphs" lang="egy-Egyp">
        {result.words
          .filter((w) => w.hieroglyphs !== "")
          .map((w, i) => (
            <span key={i} className={`glyph-word ${w.method}`} title={wordTitle(w)}>
              {w.hieroglyphs}
            </span>
          ))}
      </p>
      {(phonetic || attested) && (
        <ul className="legend small">
          {attested && (
            <li>
              <span className="swatch lexicon" aria-hidden="true" /> Attested spelling: the most frequent spelling of
              this word form in the corpus below.
            </li>
          )}
          {phonetic && (
            <li>
              <span className="swatch phonetic" aria-hidden="true" /> Spelled sign by sign from one-consonant signs
              {result.lexicon ? " (word not found in the corpus)" : ""}. Not an attested spelling: scribes combined
              multi-consonant signs, phonetic complements and classifiers.
            </li>
          )}
        </ul>
      )}
      {result.sources.map((s) => (
        <p key={s.id} className="reference">
          Data: <a href={s.url}>{s.name}</a> ({s.license}){s.attribution ? `. ${s.attribution}` : ""}
        </p>
      ))}
    </>
  );
}

function wordTitle(w: RenderedWord): string {
  if (w.method === "lexicon" && w.attestation) {
    const a = w.attestation;
    return `${w.translit}: attested ${a.count} of ${a.total} times in this spelling (${a.variants} spelling${a.variants === 1 ? "" : "s"} in the corpus)`;
  }
  const signs = (w.signs ?? []).map((s) => `${s.value} = ${s.gardiner || s.codepoint}`).join(", ");
  return `${w.translit}: phonetic spelling, not attested (${signs})`;
}

function message(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  return e instanceof Error ? e.message : String(e);
}
