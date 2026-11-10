"""The shared IPA symbol table (tts/symbols.txt) and tokenizer.

The Go runtime (package ``tts``) implements the same format and algorithm;
both are tested against the vectors in ``tts/testdata/``. See the header of
``tts/symbols.txt`` for the format and tokenization rules.
"""

from __future__ import annotations

import hashlib
import os
import re
import unicodedata
from collections.abc import Iterable, Sequence
from dataclasses import dataclass, field
from pathlib import Path

PAD = "<pad>"
SPACE = "<space>"
_SPECIAL = {PAD, SPACE}
_CODEPOINT = re.compile(r"^U\+([0-9A-F]{4,6})$")
_WS = re.compile(r"\s+")


def default_symbols_path() -> Path:
    """Locate tts/symbols.txt: $NABU_TTS_SYMBOLS, else relative to this source tree."""
    env = os.environ.get("NABU_TTS_SYMBOLS")
    if env:
        return Path(env)
    # src/nabu_tts/symbols.py -> tts/train/src/nabu_tts -> tts/symbols.txt
    return Path(__file__).resolve().parents[3] / "symbols.txt"


class SymbolTableError(ValueError):
    """The symbol table file is malformed."""


class UnknownSymbolError(ValueError):
    """An IPA string contains a code point that is not in the symbol table."""

    def __init__(self, symbol: str, offset: int, text: str) -> None:
        self.symbol = symbol
        self.offset = offset
        self.text = text
        super().__init__(
            f"IPA symbol {symbol!r} (U+{ord(symbol):04X}) at position {offset} in {text!r} "
            "is not in the symbol table (tts/symbols.txt)"
        )


@dataclass(frozen=True)
class SymbolTable:
    """Ordered symbols; ``symbols[i]`` is the canonical form of token id ``i``."""

    symbols: tuple[str, ...]
    aliases: tuple[tuple[str, ...], ...]
    _index: dict[str, int] = field(repr=False, compare=False)

    @property
    def pad_id(self) -> int:
        return self._index[PAD]

    @property
    def space_id(self) -> int:
        return self._index[SPACE]

    def __len__(self) -> int:
        return len(self.symbols)

    def __contains__(self, symbol: str) -> bool:
        return symbol in self._index

    def id(self, symbol: str) -> int:
        return self._index[symbol]

    def sha256(self) -> str:
        """Hash of the table's content (ids, symbols and aliases; not comments).

        Canonical form: one line per entry, ``symbol`` then ``\\t``-separated
        aliases, each code point written as ``U+XXXX`` (special names as-is),
        lines joined with ``\\n`` plus a trailing ``\\n``, encoded as UTF-8.
        """
        lines = []
        for sym, al in zip(self.symbols, self.aliases, strict=True):
            lines.append("\t".join(_canon(s) for s in (sym, *al)))
        return hashlib.sha256(("\n".join(lines) + "\n").encode("utf-8")).hexdigest()

    def normalize(self, ipa: str) -> str:
        """NFD-normalize, trim and collapse whitespace runs to one space."""
        return _WS.sub(" ", unicodedata.normalize("NFD", ipa)).strip()

    def tokenize(self, ipa: str) -> list[int]:
        """Map an IPA string to token ids. Raises UnknownSymbolError; never drops input."""
        text = self.normalize(ipa)
        ids = []
        for offset, ch in enumerate(text):
            key = SPACE if ch == " " else ch
            try:
                ids.append(self._index[key])
            except KeyError:
                raise UnknownSymbolError(ch, offset, text) from None
        return ids

    def unknown_symbols(self, ipa: str) -> list[str]:
        """Return the code points of ``ipa`` (after normalization) that are not in the table."""
        return [ch for ch in self.normalize(ipa) if ch != " " and ch not in self._index]

    def intersperse(self, ids: Sequence[int]) -> list[int]:
        """VITS add_blank: put pad between all tokens and at both ends."""
        out = [self.pad_id] * (len(ids) * 2 + 1)
        out[1::2] = ids
        return out


def _canon(s: str) -> str:
    return s if s in _SPECIAL else f"U+{ord(s):04X}"


def _parse_field(f: str, where: str) -> str:
    if f in _SPECIAL:
        return f
    m = _CODEPOINT.match(f)
    if m:
        return chr(int(m.group(1), 16))
    if len(f) != 1:
        raise SymbolTableError(
            f"{where}: field {f!r} must be one code point, U+XXXX, {PAD} or {SPACE}"
        )
    return f


def parse_symbols(lines: Iterable[str], source: str = "symbols.txt") -> SymbolTable:
    symbols: list[str] = []
    aliases: list[tuple[str, ...]] = []
    index: dict[str, int] = {}
    for lineno, raw in enumerate(lines, 1):
        fields = []
        for f in raw.split():
            if f.startswith("#"):
                break
            fields.append(f)
        if not fields:
            continue
        where = f"{source}:{lineno}"
        parsed = [_parse_field(f, where) for f in fields]
        for p in parsed:
            if p not in _SPECIAL and unicodedata.normalize("NFD", p) != p:
                raise SymbolTableError(f"{where}: {p!r} is not NFD-stable; list its parts instead")
            if p in index:
                raise SymbolTableError(f"{where}: {_canon(p)} already defined (id {index[p]})")
            index[p] = len(symbols)
        symbols.append(parsed[0])
        aliases.append(tuple(parsed[1:]))
    for name in (PAD, SPACE):
        if name not in index:
            raise SymbolTableError(f"{source}: missing required entry {name}")
    if index[PAD] != 0:
        raise SymbolTableError(f"{source}: {PAD} must be the first entry (id 0)")
    return SymbolTable(tuple(symbols), tuple(aliases), index)


def load_symbols(path: str | os.PathLike[str] | None = None) -> SymbolTable:
    p = Path(path) if path is not None else default_symbols_path()
    with open(p, encoding="utf-8") as f:
        return parse_symbols(f, source=str(p))
