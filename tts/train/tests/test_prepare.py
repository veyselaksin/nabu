"""End-to-end test of nabu-tts-prepare on a fake Common Voice directory (WAV clips)."""

import json

import numpy as np
import pytest

pytest.importorskip("epitran")
pytest.importorskip("miniaudio")

from nabu_tts import prepare
from nabu_tts.audio import read_wav, write_wav
from nabu_tts.commonvoice import speaker_label
from nabu_tts.manifest import read_manifest

SR_IN = 48000


def clip(seconds, amp=0.4, seed=0):
    rng = np.random.default_rng(seed)
    t = np.arange(int(SR_IN * seconds)) / SR_IN
    voiced = (amp * np.sin(2 * np.pi * 180 * t)).astype(np.float32)
    pad = (1e-4 * rng.standard_normal(int(SR_IN * 0.3))).astype(np.float32)
    return np.concatenate([pad, voiced, pad])


def test_prepare(tmp_path):
    cv = tmp_path / "xx"
    (cv / "clips").mkdir(parents=True)
    rows = [
        # client, file, sentence, up, down, seconds
        ("spkA", "a1.wav", "tata", 2, 0, 1.5),
        ("spkA", "a2.wav", "tata, tata!", 3, 1, 1.5),
        ("spkA", "a3.wav", "tata", 1, 0, 1.5),  # too few votes
        ("spkA", "a4.wav", "tata", 2, 2, 1.5),  # not positive
        ("spkB", "b1.wav", "tata", 2, 0, 0.3),  # too short
        ("spkB", "b2.wav", "ta1", 2, 0, 1.5),  # digit: unmapped
        ("spkB", "b3.wav", "tata", 2, 0, 1.5),
        ("spkC", "c1.wav", "tata", 2, 0, 1.5),  # dropped by --top-speakers 2
    ]
    with open(cv / "validated.tsv", "w", encoding="utf-8") as f:
        f.write("client_id\tpath\tsentence\tup_votes\tdown_votes\tlocale\n")
        for i, (spk, name, text, up, down, secs) in enumerate(rows):
            f.write(f"{spk}\t{name}\t{text}\t{up}\t{down}\txx\n")
            write_wav(cv / "clips" / name, clip(secs, seed=i), SR_IN)

    out = tmp_path / "out"
    rc = prepare.main(
        [
            "--cv-dir",
            str(cv),
            "--out",
            str(out),
            "--epitran",
            "mlt-Latn",
            "--sample-rate",
            "16000",
            "--top-speakers",
            "2",
            "--workers",
            "1",
        ]
    )
    assert rc == 0
    report = json.loads((out / "report.json").read_text())
    c = report["counts"]
    assert c["rows"] == 8
    assert c["rejected_votes"] == 2
    assert c["rejected_text_unmapped_chars"] == 1
    assert c["rejected_not_top_speaker"] == 1
    assert c["rejected_audio_too_short"] == 1
    assert c["kept"] == 3
    entries = list(read_manifest(out / "manifest.jsonl"))
    assert {e.speaker_id for e, _ in entries} == {speaker_label("xx", s) for s in ("spkA", "spkB")}
    for e, path in entries:
        assert e.language == "xx" and e.ipa and "," not in e.ipa and "!" not in e.ipa
        x, sr = read_wav(path)
        assert sr == 16000 and abs(len(x) / sr - e.duration) < 0.01
        assert 1.0 <= e.duration <= 12.0
