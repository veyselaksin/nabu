"""Candidate donor languages (tts/train/configs/donors.yaml) and orthography → IPA conversion."""

from __future__ import annotations

import csv
import os
import unicodedata
from collections import Counter
from dataclasses import dataclass
from pathlib import Path

import yaml

from nabu_tts.symbols import SymbolTable

DEFAULT_DONORS = Path(__file__).resolve().parents[2] / "configs" / "donors.yaml"

# Unicode character-name prefixes → ISO 15924 script codes, for the scripts
# that appear in donors.yaml.
_SCRIPT_PREFIX = {
    "ARABIC": "Arab",
    "LATIN": "Latn",
    "CYRILLIC": "Cyrl",
    "ETHIOPIC": "Ethi",
    "GEORGIAN": "Geor",
    "SYRIAC": "Syrc",
    "HEBREW": "Hebr",
    "GREEK": "Grek",
    "ARMENIAN": "Armn",
}

# Arabic-script short-vowel and related marks (fathatan … sukun, superscript alef).
ARABIC_VOWEL_MARKS = frozenset(chr(c) for c in [*range(0x064B, 0x0653), 0x0670])


@dataclass(frozen=True)
class Donor:
    cv_locale: str
    epitran_code: str
    script: str
    abjad: bool
    notes: str = ""


def load_donors(path: str | os.PathLike[str] | None = None) -> dict[str, Donor]:
    with open(path or DEFAULT_DONORS, encoding="utf-8") as f:
        raw = yaml.safe_load(f)
    out = {}
    for item in raw["candidates"]:
        d = Donor(**item)
        out[d.cv_locale] = d
    return out


def char_script(ch: str) -> str | None:
    """ISO 15924 code for a letter, or None for non-letters and unlisted scripts."""
    if not unicodedata.category(ch).startswith("L"):
        return None
    try:
        name = unicodedata.name(ch)
    except ValueError:
        return None
    return _SCRIPT_PREFIX.get(name.split(" ", 1)[0])


@dataclass
class ScriptStats:
    """Script and vowel-mark statistics over orthographic sentences."""

    letters: Counter[str]
    arabic_letters: int = 0
    arabic_vowel_marks: int = 0

    def add(self, sentence: str) -> None:
        for ch in unicodedata.normalize("NFC", sentence):
            if ch in ARABIC_VOWEL_MARKS:
                self.arabic_vowel_marks += 1
                continue
            s = char_script(ch)
            if s:
                self.letters[s] += 1
                if s == "Arab":
                    self.arabic_letters += 1

    def dominant(self) -> tuple[str | None, float]:
        total = sum(self.letters.values())
        if not total:
            return None, 0.0
        script, n = self.letters.most_common(1)[0]
        return script, n / total

    def vowel_mark_density(self) -> float | None:
        """Arabic vowel marks per Arabic letter, or None if no Arabic letters were seen."""
        if not self.arabic_letters:
            return None
        return self.arabic_vowel_marks / self.arabic_letters


def new_script_stats() -> ScriptStats:
    return ScriptStats(letters=Counter())


@dataclass(frozen=True)
class Conversion:
    ipa: str | None  # None when rejected
    reason: str | None = None  # rejection reason
    detail: str = ""


class EpitranConverter:
    """Orthography → IPA via Epitran, rejecting (never silently dropping) what it can't map.

    * Punctuation that the Epitran map does not use as a letter is replaced by
      a space before conversion (prosody from punctuation is not modelled).
    * Any letter, mark or digit that Epitran leaves unmapped rejects the
      sentence ("unmapped_chars").
    * IPA symbols missing from tts/symbols.txt reject it ("unknown_ipa_symbols").
    """

    def __init__(self, epitran_code: str, symbols: SymbolTable) -> None:
        import epitran  # optional dependency: nabu-tts[data]

        self.code = epitran_code
        self.symbols = symbols
        self._epi = epitran.Epitran(epitran_code)
        self._map_orth = _map_orthography(epitran_code)

    def _strip_punct(self, text: str) -> str:
        out = []
        for ch in text:
            if unicodedata.category(ch).startswith("P") and ch not in self._map_orth:
                out.append(" ")
            else:
                out.append(ch)
        return "".join(out)

    def convert(self, sentence: str) -> Conversion:
        text = self._strip_punct(unicodedata.normalize("NFC", sentence))
        if not text.strip():
            return Conversion(None, "empty")
        unmapped = []
        for cat, _upper, orth, phon, _segs in self._epi.word_to_tuples(text):
            if cat[:1] in ("L", "M", "N") and phon == "":
                unmapped.append(orth)
        if unmapped:
            return Conversion(None, "unmapped_chars", " ".join(sorted(set(unmapped))))
        ipa = self.symbols.normalize(self._epi.transliterate(text))
        if not ipa:
            return Conversion(None, "empty")
        unknown = self.symbols.unknown_symbols(ipa)
        if unknown:
            return Conversion(None, "unknown_ipa_symbols", " ".join(sorted(set(unknown))))
        return Conversion(ipa)


def _map_orthography(code: str) -> frozenset[str]:
    """All characters that appear in the Orth column of Epitran's map for ``code``."""
    import epitran

    path = Path(epitran.__file__).parent / "data" / "map" / f"{code}.csv"
    chars: set[str] = set()
    with open(path, encoding="utf-8") as f:
        rows = csv.reader(f)
        next(rows, None)
        for row in rows:
            if row:
                chars.update(unicodedata.normalize("NFC", row[0]))
    return frozenset(chars)


def map_outputs(code: str) -> list[str]:
    """Phon column entries of Epitran's map table for ``code`` (before pre/post rules)."""
    import epitran

    path = Path(epitran.__file__).parent / "data" / "map" / f"{code}.csv"
    with open(path, encoding="utf-8") as f:
        rows = csv.reader(f)
        next(rows, None)
        return [row[1] for row in rows if len(row) >= 2]
