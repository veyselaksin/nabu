import { useEffect, useId, useRef, useState, type FormEvent, type KeyboardEvent } from "react";
import {
  ApiError,
  listRulesets,
  render,
  speak,
  toIPA,
  translate,
  translator,
  voices,
  type DataSource,
  type G2PResult,
  type RenderResult,
  type RenderedWord,
  type RulesetInfo,
  type SpeakResult,
  type TranslateResult,
  type TranslatorInfo,
  type VoicesInfo,
} from "./api";
import { LANGUAGES, type Language } from "./languages";
import { Card, CopyButton, Icon, Notice, Spinner, StatusPill, useElapsed, type Status } from "./ui";

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

const DEFAULT_LANGUAGE: Language = LANGUAGES[0]!;

type Busy = "translate" | "convert" | "speak" | null;

interface Spoken extends SpeakResult {
  url: string;
}

export function App() {
  const ids = { english: useId(), text: useId(), ruleset: useId() };
  const textRef = useRef<HTMLTextAreaElement>(null);
  const [english, setEnglish] = useState("");
  const [translatorInfo, setTranslatorInfo] = useState<TranslatorInfo | null>(null);
  const [translation, setTranslation] = useState<TranslateResult | null>(null);
  const [translateError, setTranslateError] = useState<string | null>(null);
  const [rulesets, setRulesets] = useState<RulesetInfo[]>([]);
  const [lang, setLang] = useState<Language>(DEFAULT_LANGUAGE);
  const [ruleset, setRuleset] = useState("");
  const [text, setText] = useState(DEFAULT_LANGUAGE.examples[0] ?? "");
  const [result, setResult] = useState<G2PResult | null>(null);
  const [spoken, setSpoken] = useState<Spoken | null>(null);
  const [voice, setVoice] = useState<VoicesInfo | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [glyphs, setGlyphs] = useState<RenderResult | null>(null);
  const [glyphError, setGlyphError] = useState<string | null>(null);
  const [busy, setBusy] = useState<Busy>(null);
  const elapsed = useElapsed(busy === "translate");

  useEffect(() => {
    let cancelled = false;
    listRulesets()
      .then((list) => {
        if (cancelled) return;
        setRulesets(list);
        setRuleset((current) => current || list.find((r) => r.language === DEFAULT_LANGUAGE.code)?.id || "");
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

  const selected = rulesets.find((r) => r.id === ruleset);
  const langRulesets = rulesets.filter((r) => r.language === lang.code);
  const ready = new Set(rulesets.map((r) => r.language));

  // Switching language starts over: its own rule set, examples and outputs.
  function chooseLanguage(next: Language) {
    if (next.code === lang.code) return;
    setLang(next);
    setRuleset(rulesets.find((r) => r.language === next.code)?.id ?? "");
    setText(next.examples[0] ?? "");
    setTranslation(null);
    setTranslateError(null);
    setResult(null);
    setError(null);
    setGlyphs(null);
    setGlyphError(null);
    setSpoken(null);
  }

  // Script rendering is independent of G2P: a word without a sign must not
  // hide the IPA, and vice versa, so its errors are shown separately.
  async function refreshGlyphs(input: string) {
    try {
      setGlyphs(await render(input));
      setGlyphError(null);
    } catch (err) {
      setGlyphs(null);
      setGlyphError(explain(err, selected));
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
      setError(explain(err, selected));
    } finally {
      await glyphsDone;
      setBusy(null);
    }
  }

  function onConvert(e: FormEvent) {
    e.preventDefault();
    if (canSubmit) void convert(text);
  }

  // A translation replaces the transliteration and is converted right away.
  async function onTranslate(e?: FormEvent) {
    e?.preventDefault();
    if (!canTranslate) return;
    setBusy("translate");
    setTranslateError(null);
    let res: TranslateResult;
    try {
      res = await translate(english, lang.code);
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
      setError(explain(err, selected));
    } finally {
      await glyphsDone;
      setBusy(null);
    }
  }

  // Insert a special sign at the cursor of the transliteration field.
  function insert(sign: string) {
    const el = textRef.current;
    if (!el) return setText((t) => t + sign);
    const { selectionStart: a, selectionEnd: b } = el;
    const next = text.slice(0, a) + sign + text.slice(b);
    setText(next);
    requestAnimationFrame(() => {
      el.focus();
      el.setSelectionRange(a + sign.length, a + sign.length);
    });
  }

  // ⌘/Ctrl + Enter submits a textarea's form.
  function submitOnModEnter(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      e.currentTarget.form?.requestSubmit();
    }
  }

  const canSubmit = busy === null && ruleset !== "" && text.trim() !== "";
  const speakAvailable = voice?.available === true;
  // The smoke-test voice is trained on synthetic tones: it beeps, it does not speak.
  const testVoice = voice?.model?.languages.includes("synthetic") === true;
  const translateAvailable = translatorInfo?.available === true;
  const canTranslate = busy === null && translateAvailable && english.trim() !== "";

  const translatorStatus: Status = translatorInfo === null ? "pending" : translateAvailable ? "ok" : "off";
  const voiceStatus: Status = voice === null ? "pending" : speakAvailable ? "ok" : "off";
  const translatorHint = translateAvailable
    ? `Translation with ${translatorInfo?.model || "the configured model"}`
    : (translatorInfo?.message ?? "Checking whether translation is available…");
  const voiceHint = speakAvailable
    ? `Voice ${voice?.model?.name ?? ""}`.trim()
    : (voice?.message ?? "Checking whether speech synthesis is available…");

  return (
    <div className="app">
      <div className="backdrop" aria-hidden="true">
        <span>𓋹 𓂀 𓇳 𓆣 𓊹 𓁹 𓋴 𓅓</span>
      </div>

      <header className="topbar">
        <a className="brand" href="/" aria-label="Nabu home">
          <span className="brand-mark" aria-hidden="true">
            𓋹
          </span>
          <span className="brand-name">Nabu</span>
        </a>
        <div className="pills">
          <StatusPill label="Translator" status={translatorStatus} hint={translatorHint} />
          <StatusPill label="Voice" status={voiceStatus} hint={voiceHint} />
        </div>
      </header>

      <section className="hero">
        <p className="eyebrow">Ancient languages · reconstructed</p>
        <h1>
          From English to <em>ancient scripts</em> and sound.
        </h1>
        <p className="lede">
          Translate into a scholarly transliteration, see it in the original script, convert it to IPA with a published
          reconstruction, and hear a synthetic reading.
        </p>
        <div className="languages" role="radiogroup" aria-label="Language">
          {LANGUAGES.map((l) => {
            const available = ready.has(l.code);
            return (
              <button
                key={l.code}
                type="button"
                role="radio"
                aria-checked={l.code === lang.code}
                className={`language${l.code === lang.code ? " language-active" : ""}`}
                disabled={!available}
                onClick={() => chooseLanguage(l)}
                title={available ? `${l.name} (${l.period})` : `${l.name}: coming soon`}
              >
                {l.mark && (
                  <span className="language-mark" aria-hidden="true">
                    {l.mark}
                  </span>
                )}
                <span className="language-name">{l.name}</span>
                <span className="language-meta">{available ? l.period : "Soon"}</span>
              </button>
            );
          })}
        </div>
        <details className="disclaimer">
          <summary>
            <Icon name="info" size={16} />
            <span>
              <strong>A reconstruction, not a recording.</strong> Educational tool, not ground truth.
            </span>
            <span className="more">
              Read more <Icon name="chevron" size={14} />
            </span>
          </summary>
          <p>{DISCLAIMER}</p>
        </details>
      </section>

      <main className="workspace">
        <div className="column">
          <Card step="1" title="English" subtitle={`Translate into ${lang.period}`}>
            <form onSubmit={(e) => void onTranslate(e)} className="stack">
              <label htmlFor={ids.english} className="visually-hidden">
                English text
              </label>
              <textarea
                id={ids.english}
                className="field field-lg"
                value={english}
                onChange={(e) => setEnglish(e.target.value)}
                onKeyDown={submitOnModEnter}
                rows={3}
                placeholder="May the king live"
              />
              <div className="chips" aria-label="Examples">
                {lang.englishExamples.map((ex) => (
                  <button key={ex} type="button" className="chip" onClick={() => setEnglish(ex)}>
                    {ex}
                  </button>
                ))}
              </div>
              <div className="row">
                <button type="submit" className="btn btn-primary" disabled={!canTranslate} title={translatorHint}>
                  {busy === "translate" ? <Spinner /> : <Icon name="sparkles" />}
                  {busy === "translate" ? `Translating… ${elapsed}s` : "Translate"}
                </button>
                <span className="kbd-hint">
                  <kbd>⌘</kbd>
                  <kbd>↵</kbd>
                </span>
              </div>
              {busy === "translate" && (
                <div className="progress" role="status">
                  <span />
                  <p>Searching the corpus and asking {translatorInfo?.model ?? "the model"}. On CPU this can take a minute.</p>
                </div>
              )}
              {translatorInfo && !translateAvailable && <Notice kind="info">{translatorHint}</Notice>}
              {translateError && <Notice kind="error">{translateError}</Notice>}
            </form>
            {translation && !translateError && <Translation result={translation} />}
          </Card>

          <Card step="2" title="Transliteration" subtitle={lang.convention}>
            <form onSubmit={onConvert} className="stack">
              <label htmlFor={ids.text} className="visually-hidden">
                Transliteration
              </label>
              <textarea
                id={ids.text}
                ref={textRef}
                className="field field-translit"
                value={text}
                onChange={(e) => setText(e.target.value)}
                onKeyDown={submitOnModEnter}
                rows={2}
                spellCheck={false}
                autoCapitalize="off"
                placeholder={lang.examples[0] ?? ""}
                lang={`${lang.code}-Latn`}
              />
              <div className="keys" aria-label="Insert a transliteration sign">
                {lang.keys.map((s) => (
                  <button key={s} type="button" className="key" onClick={() => insert(s)} aria-label={`Insert ${s}`}>
                    {s}
                  </button>
                ))}
              </div>
              <div className="chips" aria-label="Examples">
                {lang.examples.map((ex) => (
                  <button key={ex} type="button" className="chip chip-translit" onClick={() => setText(ex)}>
                    {ex}
                  </button>
                ))}
              </div>

              <label htmlFor={ids.ruleset} className="field-label">
                Reconstruction
              </label>
              <div className="select-wrap">
                <select
                  id={ids.ruleset}
                  className="field"
                  value={ruleset}
                  onChange={(e) => setRuleset(e.target.value)}
                  disabled={langRulesets.length === 0}
                >
                  {langRulesets.length === 0 && <option value="">{rulesets.length ? "No rule set yet" : "Loading…"}</option>}
                  {langRulesets.map((r) => (
                    <option key={r.id} value={r.id}>
                      {r.name} ({r.id} v{r.version})
                    </option>
                  ))}
                </select>
                <Icon name="chevron" size={16} />
              </div>
              {selected && (
                <p className="reference">
                  <Icon name="book" size={14} /> {selected.reference}
                </p>
              )}

              <div className="row">
                <button type="submit" className="btn btn-primary" disabled={!canSubmit}>
                  {busy === "convert" ? <Spinner /> : <Icon name="arrow" />}
                  {busy === "convert" ? "Converting…" : "Convert"}
                </button>
                <button
                  type="button"
                  className="btn btn-secondary"
                  onClick={() => void onSpeak()}
                  disabled={!canSubmit || !speakAvailable}
                  title={voiceHint}
                >
                  {busy === "speak" ? <Spinner /> : <Icon name="play" />}
                  {busy === "speak" ? "Speaking…" : "Speak"}
                </button>
              </div>
              {voice && !speakAvailable && <Notice kind="info">{voiceHint}</Notice>}
              {testVoice && (
                <Notice kind="info">
                  The loaded voice ({voice?.model?.name}) is a pipeline test trained on synthetic tones: it beeps instead of
                  speaking. A real voice has not been trained yet.
                </Notice>
              )}
            </form>
          </Card>
        </div>

        <div className="column">
          <Card
            title={lang.script}
            subtitle="Attested spellings where the corpus has them"
            className="card-glyphs"
            actions={glyphs && !glyphError ? <CopyButton text={glyphs.hieroglyphs} label={lang.script.toLowerCase()} /> : null}
          >
            <div aria-live="polite">
              {glyphError && <Notice kind="error">{glyphError}</Notice>}
              {glyphs && !glyphError && <Hieroglyphs result={glyphs} />}
              {!glyphs && !glyphError && (
                <div className="empty">
                  <span className="empty-glyph" aria-hidden="true">
                    𓀀𓏏𓈖
                  </span>
                  <p>Press Convert or Speak to write the text in {lang.script.toLowerCase()}.</p>
                </div>
              )}
            </div>
          </Card>

          <Card
            title="Pronunciation"
            subtitle="IPA from the selected reconstruction"
            actions={result && !error ? <CopyButton text={result.ipa} label="IPA" /> : null}
          >
            <div aria-live="polite">
              {error && <Notice kind="error">{error}</Notice>}
              {result && !error && (
                <>
                  <output className="ipa">{result.ipa === "" ? "∅" : `/${result.ipa}/`}</output>
                  <p className="reference">
                    <Icon name="book" size={14} /> {result.ruleset}: {result.reference}
                  </p>
                </>
              )}
              {spoken && !error && (
                <figure className="player">
                  <div className="player-head">
                    <span className="player-badge">
                      <Icon name="wave" size={16} />
                    </span>
                    <div>
                      <strong>{testVoice ? "Test voice (synthetic tones)" : "Synthetic reading"}</strong>
                      <span className="muted">
                        {spoken.model}, speaker {spoken.speaker}
                      </span>
                    </div>
                  </div>
                  <audio controls autoPlay src={spoken.url}>
                    Your browser cannot play WAV audio.
                  </audio>
                  <figcaption className="reference">
                    Reconstruction {spoken.ruleset} v{spoken.rulesetVersion}. A modern-language voice reading reconstructed
                    IPA, not a recording.
                  </figcaption>
                </figure>
              )}
              {!result && !error && (
                <div className="empty">
                  <span className="empty-glyph empty-ipa" aria-hidden="true">
                    /ʕanχ/
                  </span>
                  <p>Convert a transliteration to see its reconstructed pronunciation.</p>
                </div>
              )}
            </div>
          </Card>
        </div>
      </main>

      <footer className="footer">
        <p>
          <strong>Nabu</strong> is open source. Every result names the reconstruction and the corpus it comes from.
        </p>
        <p className="muted">{DISCLAIMER}</p>
      </footer>
    </div>
  );
}

function Translation({ result }: { result: TranslateResult }) {
  const attested = result.words.filter((w) => w.attested).length;
  const foreign = result.words.filter((w) => (w.foreign?.length ?? 0) > 0);
  const share = result.words.length ? attested / result.words.length : 0;
  const unmatched = (result.coverage ?? []).filter((c) => !c.matched).map((c) => c.term);
  const evidenceLabel: Record<string, string> = {
    dictionary: "dictionary entry for a word of your text",
    example: "occurs in a retrieved example",
    corpus: "occurs in the corpus, not in what was retrieved",
    none: "no support found",
  };
  return (
    <div className="translation">
      <div className="translation-head">
        <p className="translit" lang="egy-Latn">
          {result.words.map((w, i) => {
            const kind = w.foreign?.length ? "foreign" : w.attested ? "attested" : "unattested";
            const title = w.foreign?.length
              ? `${w.form}: uses ${w.foreign.map((g) => `“${g}”`).join(", ")}, which the corpus never writes`
              : w.attested
                ? `${w.form}: ${w.count}× in the corpus`
                : `${w.form}: not found in the corpus`;
            return (
              <span key={i} className={`word word-${kind}`} title={title}>
                {w.form}
                {w.attested && <sup>{w.count}</sup>}
              </span>
            );
          })}
        </p>
        <CopyButton text={result.transliteration} label="transliteration" />
      </div>

      <div className="meter" aria-label={`Vocabulary check: ${attested} of ${result.words.length} words occur in the corpus`}>
        <div className="meter-bar">
          <span style={{ width: `${Math.round(share * 100)}%` }} />
        </div>
        <span className="meter-label">
          <strong>
            {attested}/{result.words.length}
          </strong>{" "}
          words attested in the corpus
        </span>
      </div>
      <ul className="glossary" aria-label="What each output word means according to the dictionary">
        {result.words.map((w, i) => (
          <li key={i}>
            <span className="translit-sm" lang="egy-Latn">
              {w.form}
            </span>
            <span>{w.gloss || <span className="muted">not in the lemmatized corpus</span>}</span>
            <span className="provenance">{evidenceLabel[w.evidence] ?? w.evidence}</span>
          </li>
        ))}
      </ul>
      {unmatched.length > 0 && (
        <p className="muted" role="note">
          No output word could be tied through the dictionary to: <strong>{unmatched.join(", ")}</strong>. These may be
          missing or mistranslated.
        </p>
      )}
      <p className="muted">
        Support score {Math.round(result.confidence * 100)}%: how well the chosen words are backed by retrieved
        evidence and cover your text. It does not check grammar or word order.
      </p>
      <ul className="legend">
        <li>
          <span className="swatch swatch-attested" /> attested
        </li>
        <li>
          <span className="swatch swatch-unattested" /> not found, possibly invented
        </li>
        {foreign.length > 0 && (
          <li>
            <span className="swatch swatch-foreign" /> not valid transliteration
          </li>
        )}
      </ul>
      <p className="reference">This checks vocabulary only, not grammar or meaning.</p>

      {foreign.length > 0 && (
        <Notice kind="warn">
          {foreign.map((w) => w.form).join(", ")} {foreign.length === 1 ? "uses" : "use"} letters the TLA never writes, so{" "}
          {foreign.length === 1 ? "it" : "they"} cannot be written in the original script or pronounced. Edit the transliteration
          below (for example yod is “ꞽ”).
        </Notice>
      )}
      {result.corrected && (
        <p className="reference">
          The model’s first answer used letters outside the TLA alphabet; it was asked to correct them.
        </p>
      )}
      {result.notes && (
        <blockquote className="model-note">
          <span>Model note</span>
          {result.notes}
        </blockquote>
      )}

      <details className="examples">
        <summary>
          <Icon name="chevron" size={14} />
          {result.examples.length} example sentence{result.examples.length === 1 ? "" : "s"} given to the model
          {result.keywords.length > 0 && (
            <span className="keywords">
              {result.keywords.map((k) => (
                <span key={k} lang="de">
                  {k}
                </span>
              ))}
            </span>
          )}
        </summary>
        <ol>
          {result.examples.map((ex, i) => (
            <li key={i}>
              <span className="translit-sm" lang="egy-Latn">
                {ex.transliteration}
              </span>
              <span className="muted" lang="de">
                {ex.translation}
              </span>
              {ex.source && (
                <span className="provenance">
                  {ex.id ?? ex.source}
                  {ex.date ? ` · ${ex.date}` : ""}
                </span>
              )}
            </li>
          ))}
        </ol>
        <p className="reference">
          {result.retrieval === "hybrid"
            ? "Found by meaning (embeddings) and by German keywords. "
            : "Found by German keywords. "}
          The corpus translations are German, as published by the editors.
          {result.retrieval_note && ` Vector search was not used: ${result.retrieval_note}.`}
        </p>
      </details>
      {result.glossary && result.glossary.length > 0 && (
        <details className="examples">
          <summary>
            <Icon name="chevron" size={14} />
            {result.glossary.length} dictionary entr{result.glossary.length === 1 ? "y" : "ies"} given to the model
          </summary>
          <ul className="glossary">
            {result.glossary.map((g) => (
              <li key={g.id}>
                <span className="translit-sm" lang="egy-Latn">
                  {g.lemma}
                </span>
                <span>
                  {g.en || <span className="muted" lang="de">{g.de}</span>}
                  {g.en_source && <span className="muted"> (English: {g.en_source})</span>}
                </span>
                <span className="provenance">
                  {g.id}
                  {g.pos ? ` · ${g.pos}` : ""}
                </span>
              </li>
            ))}
          </ul>
        </details>
      )}
      <Sources sources={result.sources} prefix={`Model: ${result.model}. Examples and vocabulary`} />
    </div>
  );
}

function Hieroglyphs({ result }: { result: RenderResult }) {
  const phonetic = result.words.some((w) => w.method === "phonetic" && w.hieroglyphs !== "");
  const attested = result.words.some((w) => w.method === "lexicon");
  return (
    <>
      <div className="glyph-panel">
        <p className="glyphs" lang="egy-Egyp">
          {result.words
            .filter((w) => w.hieroglyphs !== "")
            .map((w, i) => (
              <span key={i} className={`glyph-word ${w.method}`} title={wordTitle(w)}>
                <span className="glyph">{w.hieroglyphs}</span>
                <span className="glyph-translit" lang="egy-Latn">
                  {w.translit}
                </span>
              </span>
            ))}
        </p>
      </div>
      {(phonetic || attested) && (
        <ul className="legend legend-stack">
          {attested && (
            <li>
              <span className="swatch swatch-attested" /> Attested spelling: the most frequent spelling of this word form
              in the corpus.
            </li>
          )}
          {phonetic && (
            <li>
              <span className="swatch swatch-unattested" /> Spelled sign by sign from one-consonant signs
              {result.lexicon ? " (word not found in the corpus)" : ""}. Not an attested spelling: scribes combined
              multi-consonant signs, phonetic complements and classifiers.
            </li>
          )}
        </ul>
      )}
      <Sources sources={result.sources} prefix="Data" />
    </>
  );
}

function Sources({ sources, prefix }: { sources: DataSource[]; prefix: string }) {
  if (sources.length === 0) return null;
  return (
    <details className="sources">
      <summary>
        <Icon name="book" size={14} /> Sources and license
      </summary>
      {sources.map((s) => (
        <p key={s.id} className="reference">
          {prefix}: <a href={s.url}>{s.name}</a> ({s.license}){s.attribution ? `. ${s.attribution}` : ""}
        </p>
      ))}
    </details>
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

// Readable messages for the errors a user can fix or should understand; the
// API's own message stays available on hover.
function explain(e: unknown, rs: RulesetInfo | undefined): string {
  if (e instanceof ApiError) {
    const g = typeof e.details?.grapheme === "string" ? e.details.grapheme : "";
    const word = typeof e.details?.word === "string" ? e.details.word : "";
    if (e.code === "unknown_grapheme" && g) {
      const placeholder = rs?.version.includes("placeholder")
        ? " This rule set is still a placeholder that covers only a few signs; the full rules from its reference are not in yet."
        : "";
      return `“${g}” is not covered by the ${rs?.id ?? "selected"} reconstruction.${placeholder}`;
    }
    if (e.code === "no_sign" && g) {
      return `No hieroglyph is defined for “${g}”${word ? ` in “${word}”` : ""}, and the word is not in the corpus lexicon.`;
    }
  }
  return message(e);
}

function message(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  return e instanceof Error ? e.message : String(e);
}
