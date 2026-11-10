"""nabu-tts-coverage: which donor languages contain each IPA symbol a rule file can emit.

Three sources of evidence, from strongest to weakest:

  --manifest M.jsonl ...     prepared training data (after all filters).
  --cv LOCALE=DIR ...        Common Voice validated.tsv, converted with Epitran
                             (vote filter applied; no audio filtering). Counts
                             are per clip, so a sentence recorded 5 times counts 5x.
  --epitran-maps             Epitran's map tables only (no data). An upper bound:
                             "this mapping can output the symbol", not "the
                             language's text contains it". Pre/post rules ignored.

Writes a Markdown report (stdout or --out) and optionally JSON (--json).
The report does not choose a language mix; it informs that decision.
"""

from __future__ import annotations

import argparse
import json
import sys
from collections import Counter
from dataclasses import dataclass, field
from pathlib import Path

from nabu_tts.commonvoice import read_validated, votes_ok
from nabu_tts.donors import Donor, ScriptStats, load_donors, map_outputs, new_script_stats
from nabu_tts.manifest import read_manifest
from nabu_tts.rules import RuleOutputs, read_rule_outputs
from nabu_tts.symbols import SymbolTable, load_symbols

DEFAULT_RULES = Path(__file__).resolve().parents[4] / "g2p" / "rules" / "egy-allen.yaml"


@dataclass
class LangStats:
    source: str
    clips: int = 0
    tokens: int = 0
    symbol_counts: Counter[str] = field(default_factory=Counter)
    segment_counts: Counter[str] = field(default_factory=Counter)
    rejected: Counter[str] = field(default_factory=Counter)
    unknown_symbols: Counter[str] = field(default_factory=Counter)
    script: ScriptStats = field(default_factory=new_script_stats)
    hours: float | None = None


def _count_ipa(st: LangStats, ipa: str, segments: tuple[str, ...], symbols: SymbolTable) -> None:
    st.clips += 1
    for ch in ipa:
        if ch != " ":
            st.tokens += 1
            st.symbol_counts[ch] += 1
    for seg in segments:
        norm = symbols.normalize(seg)
        if len(norm) > 1:  # single code points are already in symbol_counts
            st.segment_counts[seg] += ipa.count(norm)


def from_manifests(
    paths: list[str], rules: RuleOutputs, symbols: SymbolTable
) -> dict[str, LangStats]:
    stats: dict[str, LangStats] = {}
    for p in paths:
        for e, _ in read_manifest(p):
            st = stats.setdefault(e.language, LangStats(source="manifest", hours=0.0))
            st.hours = (st.hours or 0.0) + e.duration / 3600
            _count_ipa(st, symbols.normalize(e.ipa), rules.segments, symbols)
            st.script.add(e.text)
    return stats


def from_common_voice(
    specs: list[str],
    rules: RuleOutputs,
    symbols: SymbolTable,
    donors: dict[str, Donor],
    min_up_votes: int,
    max_rows: int,
) -> dict[str, LangStats]:
    from nabu_tts.donors import EpitranConverter

    stats: dict[str, LangStats] = {}
    for spec in specs:
        locale, _, d = spec.partition("=")
        if not d:
            raise SystemExit(f"--cv expects LOCALE=DIR, got {spec!r}")
        donor = donors.get(locale)
        if donor is None:
            raise SystemExit(f"--cv {locale}: not in donors.yaml (add it with its Epitran code)")
        conv = EpitranConverter(donor.epitran_code, symbols)
        st = stats[locale] = LangStats(source=f"common_voice text ({donor.epitran_code})")
        cache: dict[str, object] = {}
        for i, clip in enumerate(read_validated(d)):
            if max_rows and i >= max_rows:
                break
            if not votes_ok(clip, min_up_votes):
                st.rejected["votes"] += 1
                continue
            st.script.add(clip.sentence)
            c = cache.get(clip.sentence)
            if c is None:
                c = cache[clip.sentence] = conv.convert(clip.sentence)
            if c.ipa is None:
                st.rejected[c.reason] += 1
                if c.reason == "unknown_ipa_symbols":
                    st.unknown_symbols.update(c.detail.split())
                continue
            _count_ipa(st, c.ipa, rules.segments, symbols)
        print(f"  {locale}: {st.clips} clips converted", file=sys.stderr)
    return stats


def from_epitran_maps(donors: dict[str, Donor], symbols: SymbolTable) -> dict[str, set[str]]:
    out = {}
    for locale, d in donors.items():
        chars: set[str] = set()
        for phon in map_outputs(d.epitran_code):
            chars.update(symbols.normalize(phon))
        out[locale] = chars
    return out


def _sym(ch: str) -> str:
    return f"`{ch}` U+{ord(ch):04X}" if ch.isprintable() else f"U+{ord(ch):04X}"


def render(
    rules: RuleOutputs,
    symbols: SymbolTable,
    targets: list[str],
    stats: dict[str, LangStats],
    maps: dict[str, set[str]] | None,
    donors: dict[str, Donor],
    low: int,
) -> tuple[str, dict]:
    out: list[str] = []
    w = out.append
    w(f"# IPA coverage report: `{rules.id}` v{rules.version}\n")
    if rules.placeholder:
        w(
            "> [!WARNING]\n> This rule file is marked PLACEHOLDER. Its outputs are toy values, so "
            "this report only shows which of *those* symbols donors cover. Re-run it after the "
            "rules are sourced.\n"
        )
    missing = [ch for ch in targets if ch not in symbols]
    if missing:
        w(
            "**ERROR: the rule file can emit symbols that are not in tts/symbols.txt:** "
            + ", ".join(_sym(c) for c in missing)
            + ". The runtime will refuse these. Append them to the table.\n"
        )
    w(f"Target symbols ({len(targets)}): " + " ".join(f"`{c}`" for c in targets) + "\n")
    multi = [s for s in rules.segments if len(symbols.normalize(s)) > 1]
    if multi:
        w(
            "Multi-symbol outputs (counted as substrings): "
            + ", ".join(f"`{s}`" for s in multi)
            + "\n"
        )

    result: dict = {
        "ruleset": {"id": rules.id, "version": rules.version, "placeholder": rules.placeholder},
        "targets": targets,
        "low_threshold": low,
        "languages": {},
        "flags": {},
    }
    langs = sorted(stats)
    if stats:
        w("## Counts in converted data\n")
        w(
            f"Occurrences per language. **ZERO** = no donor has it; **LOW** = total below {low} "
            "(`--low`).\n"
        )
        w("| symbol | " + " | ".join(langs) + " | total | flag |")
        w("|---|" + "---:|" * len(langs) + "---:|---|")
        for ch in targets + multi:
            per = [
                stats[lang].symbol_counts[ch] if len(ch) == 1 else stats[lang].segment_counts[ch]
                for lang in langs
            ]
            total = sum(per)
            flag = "**ZERO**" if total == 0 else ("**LOW**" if total < low else "")
            if flag:
                result["flags"][ch] = flag.strip("*")
            w(
                f"| {_sym(ch) if len(ch) == 1 else f'`{ch}`'} | "
                + " | ".join(f"{n:,}" for n in per)
                + f" | {total:,} | {flag} |"
            )
        w("")
        w("## Per-language summary\n")
        w(
            "| language | source | clips | hours | IPA tokens | targets present | "
            "dominant script | Arabic vowel marks / letter | rejected |"
        )
        w("|---|---|---:|---:|---:|---:|---|---:|---|")
        for lang in langs:
            st = stats[lang]
            present = sum(1 for ch in targets if st.symbol_counts[ch])
            script, share = st.script.dominant()
            d = donors.get(lang)
            script_cell = f"{script or '?'} ({share:.0%})"
            if d and script and script != d.script:
                script_cell += f" ≠ Epitran {d.script}"
            density = st.script.vowel_mark_density()
            rej = ", ".join(f"{k}: {v:,}" for k, v in st.rejected.most_common()) or "-"
            hours = f"{st.hours:.1f}" if st.hours is not None else "-"
            w(
                f"| {lang} | {st.source} | {st.clips:,} | {hours} | {st.tokens:,} | "
                f"{present}/{len(targets)} | {script_cell} | "
                f"{'-' if density is None else f'{density:.3f}'} | {rej} |"
            )
            result["languages"][lang] = {
                "source": st.source,
                "clips": st.clips,
                "hours": st.hours,
                "tokens": st.tokens,
                "counts": {ch: st.symbol_counts[ch] for ch in targets},
                "segment_counts": dict(st.segment_counts),
                "dominant_script": script,
                "arabic_vowel_marks_per_letter": density,
                "rejected": dict(st.rejected),
                "unknown_symbols": dict(st.unknown_symbols),
            }
        w("")
        unk = {lang: stats[lang].unknown_symbols for lang in langs if stats[lang].unknown_symbols}
        if unk:
            w("## Epitran output not in tts/symbols.txt\n")
            w("Sentences with these were rejected: add them to the table or accept the loss.\n")
            for lang, c in unk.items():
                w(f"- **{lang}**: " + ", ".join(f"{_sym(k)} x{v:,}" for k, v in c.most_common(20)))
            w("")

    if maps is not None:
        w("## Epitran map tables (upper bound, no data)\n")
        w(
            "✓ = the language's Epitran map table can output the symbol. This does not mean "
            "the symbol occurs in its text.\n"
        )
        locs = sorted(maps)
        w("| symbol | " + " | ".join(locs) + " |")
        w("|---|" + ":-:|" * len(locs))
        for ch in targets:
            w(
                f"| {_sym(ch)} | "
                + " | ".join("✓" if ch in maps[loc] else "" for loc in locs)
                + " |"
            )
            result.setdefault("epitran_maps", {})[ch] = [loc for loc in locs if ch in maps[loc]]
        w("")

    abjads = [d for d in donors.values() if d.abjad and (d.cv_locale in stats or maps)]
    if abjads:
        w("## Unvocalized scripts\n")
        w(
            "These candidates are marked `abjad: true`: their usual orthography omits short "
            "vowels, so Epitran's IPA lacks them and the audio does not match the labels. "
            "`nabu-tts-prepare` refuses them unless `--allow-abjad` is passed. See "
            "tts/train/README.md for options.\n"
        )
        for d in abjads:
            w(f"- **{d.cv_locale}** ({d.epitran_code}): {d.notes}")
        w("")
    return "\n".join(out), result


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(prog="nabu-tts-coverage", description=__doc__.split("\n")[0])
    ap.add_argument("--rules", default=str(DEFAULT_RULES), help="G2P rule file")
    ap.add_argument("--symbols", help="symbol table (default: tts/symbols.txt)")
    ap.add_argument("--donors", help="donor candidates YAML (default: configs/donors.yaml)")
    ap.add_argument("--manifest", nargs="*", default=[], help="prepared manifests")
    ap.add_argument("--cv", nargs="*", default=[], metavar="LOCALE=DIR", help="Common Voice dirs")
    ap.add_argument("--epitran-maps", action="store_true", help="add the map-table upper bound")
    ap.add_argument("--min-up-votes", type=int, default=2)
    ap.add_argument("--max-rows", type=int, default=0, help="per --cv locale; 0 = all")
    ap.add_argument("--low", type=int, default=200, help="flag totals below this as LOW")
    ap.add_argument("--out", help="write Markdown here instead of stdout")
    ap.add_argument("--json", help="also write machine-readable results here")
    args = ap.parse_args(argv)
    if not (args.manifest or args.cv or args.epitran_maps):
        ap.error("give at least one of --manifest, --cv, --epitran-maps")

    symbols = load_symbols(args.symbols)
    rules = read_rule_outputs(args.rules)
    donors = load_donors(args.donors)
    targets = rules.code_points(symbols)

    stats: dict[str, LangStats] = {}
    if args.manifest:
        stats.update(from_manifests(args.manifest, rules, symbols))
    if args.cv:
        cv = from_common_voice(args.cv, rules, symbols, donors, args.min_up_votes, args.max_rows)
        clash = set(cv) & set(stats)
        if clash:
            ap.error(f"languages given both as --manifest and --cv: {sorted(clash)}")
        stats.update(cv)
    maps = from_epitran_maps(donors, symbols) if args.epitran_maps else None

    md, result = render(rules, symbols, targets, stats, maps, donors, args.low)
    if args.out:
        Path(args.out).write_text(md + "\n", encoding="utf-8")
        print(f"wrote {args.out}", file=sys.stderr)
    else:
        print(md)
    if args.json:
        Path(args.json).write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n")
    missing = [ch for ch in targets if ch not in symbols]
    return 1 if missing else 0


if __name__ == "__main__":
    raise SystemExit(main())
