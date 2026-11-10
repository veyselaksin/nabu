from pathlib import Path

import pytest

from nabu_tts.config import config_from_dict, load_config

CONFIGS = Path(__file__).resolve().parents[1] / "configs"


def test_smoke_config_loads():
    cfg = load_config(CONFIGS / "smoke.yaml")
    assert cfg.train.device == "cpu"
    assert cfg.train.learning_rate == pytest.approx(2e-4)


def test_full_config_requires_a_manifest_choice():
    # The donor mix is a project decision, so full.yaml ships without manifests.
    with pytest.raises(ValueError, match="manifests"):
        load_config(CONFIGS / "full.yaml")


def test_validation():
    base = {"data": {"manifests": ["m.jsonl"], "hop_length": 256}}
    with pytest.raises(ValueError, match="unknown keys"):
        config_from_dict({**base, "bogus": 1})
    with pytest.raises(ValueError, match="segment_size"):
        config_from_dict({**base, "train": {"segment_size": 1000}})
    with pytest.raises(ValueError, match="upsample_rates"):
        config_from_dict({**base, "model": {"upsample_rates": [8, 8]}})
    cfg = config_from_dict({**base, "train": {"learning_rate": "2e-4"}})
    assert cfg.train.learning_rate == pytest.approx(2e-4)
