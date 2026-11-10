import json
from pathlib import Path

from nabu_tts import coverage
from nabu_tts.manifest import Entry, write_manifest

RULES = """
id: test-cov
language: egy
name: Coverage fixture
reference: none
version: "1"
graphemes:
  - { match: "a", ipa: "ʕ" }
  - { match: "b", ipa: "n" }
  - { match: "c", ipa: "tʃ" }
  - { match: "d", ipa: "q" }
  - { match: ".", ipa: "" }
"""


def test_report_counts_and_flags(tmp_path: Path):
    rules = tmp_path / "rules.yaml"
    rules.write_text(RULES, encoding="utf-8")
    write_manifest(
        tmp_path / "m.jsonl",
        [
            Entry("a.wav", "ʕa ntʃ", "xx", "xx-1", 1.0, text="abc"),
            Entry("b.wav", "ʕʕ n", "yy", "yy-1", 2.0, text="abc"),
        ],
    )
    out_md, out_json = tmp_path / "r.md", tmp_path / "r.json"
    rc = coverage.main(
        [
            "--rules",
            str(rules),
            "--manifest",
            str(tmp_path / "m.jsonl"),
            "--low",
            "3",
            "--out",
            str(out_md),
            "--json",
            str(out_json),
        ]
    )
    assert rc == 0
    result = json.loads(out_json.read_text())
    assert result["targets"] == ["ʕ", "n", "t", "ʃ", "q"]
    assert result["languages"]["xx"]["counts"]["ʕ"] == 1
    assert result["languages"]["yy"]["counts"]["ʕ"] == 2
    assert result["languages"]["xx"]["segment_counts"] == {"tʃ": 1}
    assert result["flags"] == {"n": "LOW", "t": "LOW", "ʃ": "LOW", "q": "ZERO", "tʃ": "LOW"}
    md = out_md.read_text()
    assert "**ZERO**" in md and "| xx |" in md


def test_missing_symbol_is_an_error(tmp_path: Path):
    rules = tmp_path / "rules.yaml"
    rules.write_text(RULES.replace('"q"', '"ʧ"'), encoding="utf-8")
    write_manifest(tmp_path / "m.jsonl", [Entry("a.wav", "n", "xx", "xx-1", 1.0)])
    rc = coverage.main(
        [
            "--rules",
            str(rules),
            "--manifest",
            str(tmp_path / "m.jsonl"),
            "--out",
            str(tmp_path / "r.md"),
        ]
    )
    assert rc == 1
    assert "not in tts/symbols.txt" in (tmp_path / "r.md").read_text()
