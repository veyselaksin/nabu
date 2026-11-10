"""Reading the IPA outputs of a G2P rule file (g2p/rules/*.yaml)."""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path

import yaml

from nabu_tts.symbols import SymbolTable


@dataclass(frozen=True)
class RuleOutputs:
    id: str
    version: str
    name: str
    reference: str
    # Every distinct non-empty `ipa` value, in file order.
    segments: tuple[str, ...]

    @property
    def placeholder(self) -> bool:
        return "PLACEHOLDER" in self.name.upper() or "PLACEHOLDER" in self.reference.upper()

    def code_points(self, symbols: SymbolTable) -> list[str]:
        """Distinct code points (after the tokenizer's normalization) the rules can emit."""
        seen: dict[str, None] = {}
        for seg in self.segments:
            for ch in symbols.normalize(seg):
                if ch != " ":
                    seen.setdefault(ch)
        return list(seen)


def read_rule_outputs(path: str | Path) -> RuleOutputs:
    """Collect every IPA string a rule file can output (context does not matter here)."""
    with open(path, encoding="utf-8") as f:
        raw = yaml.safe_load(f)
    segs: dict[str, None] = {}
    for g in raw.get("graphemes") or []:
        ipa = g.get("ipa")
        if ipa:
            segs.setdefault(ipa)
    return RuleOutputs(
        id=raw["id"],
        version=str(raw["version"]),
        name=raw["name"],
        reference=str(raw["reference"]).strip(),
        segments=tuple(segs),
    )
