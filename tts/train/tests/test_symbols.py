import json
from pathlib import Path

import pytest

from nabu_tts.symbols import (
    SymbolTableError,
    UnknownSymbolError,
    load_symbols,
    parse_symbols,
)

TESTDATA = Path(__file__).resolve().parents[2] / "testdata"


def vectors():
    with open(TESTDATA / "tokenize.jsonl", encoding="utf-8") as f:
        for line in f:
            v = json.loads(line)
            if "comment" not in v:
                yield v


@pytest.mark.parametrize("v", list(vectors()), ids=lambda v: repr(v["input"]))
def test_shared_vectors(v):
    """Same vectors as tts/symbols_test.go: Go and Python must tokenize identically."""
    table = load_symbols()
    if "error" in v:
        with pytest.raises(UnknownSymbolError) as exc:
            table.tokenize(v["input"])
        assert f"U+{ord(exc.value.symbol):04X}" == v["error"]["symbol"]
        assert exc.value.offset == v["error"]["offset"]
        return
    ids = table.tokenize(v["input"])
    if v["add_blank"]:
        ids = table.intersperse(ids)
    assert ids == v["ids"]


def test_hash_matches_go():
    table = load_symbols(TESTDATA / "symbols-test.txt")
    assert table.sha256() == (TESTDATA / "symbols-test.sha256").read_text().strip()
    assert table.tokenize("g ã") == [4, 1, 2, 3]


def test_default_table_covers_rule_outputs():
    table = load_symbols()
    assert table.pad_id == 0
    for ch in "ʕħχntpfr":
        assert ch in table


@pytest.mark.parametrize(
    "src",
    [
        "<space>\na\n",
        "<space>\n<pad>\n",
        "<pad>\n<space>\na\nU+0061\n",
        "<pad>\n<space>\nab\n",
        "<pad>\n<space>\nã\n",
        "<pad>\n<space>\n<pad>\n",
    ],
)
def test_parse_errors(src):
    with pytest.raises(SymbolTableError):
        parse_symbols(src.splitlines())


def test_unknown_symbols_lists_everything():
    table = load_symbols()
    assert table.unknown_symbols("nAʧ") == ["A", "ʧ"]
