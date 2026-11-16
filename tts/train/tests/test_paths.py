from pathlib import Path

import pytest

from nabu_tts import paths
from nabu_tts.config import config_from_dict, load_config

CONFIGS = Path(__file__).resolve().parents[1] / "configs"
VARS = ("NABU_HOME", "MODELS_ROOT", "NABU_DATA_DIR", "NABU_RUNS_DIR", "NABU_CHECKPOINT_DIR")


@pytest.fixture
def env(monkeypatch):
    for v in VARS:
        monkeypatch.delenv(v, raising=False)
    return monkeypatch


def test_repository_defaults(env):
    assert paths.data_dir() == Path("data/corpora")
    assert paths.runs_dir() == Path("runs")
    assert paths.checkpoint_dir() == Path("runs/checkpoints")


def test_models_root(env, tmp_path):
    env.setenv("MODELS_ROOT", str(tmp_path))
    home = tmp_path / "projects" / "nabu"
    assert paths.data_dir() == home / "datasets"
    assert paths.runs_dir() == home / "training-runs"
    assert paths.checkpoint_dir() == home / "checkpoints"
    env.setenv("NABU_HOME", "/h")
    assert paths.runs_dir() == Path("/h/training-runs")
    env.setenv("NABU_RUNS_DIR", "/r")
    assert paths.runs_dir() == Path("/r")


def test_unmounted_models_root_is_an_error(env, tmp_path):
    env.setenv("MODELS_ROOT", str(tmp_path / "unmounted"))
    with pytest.raises(RuntimeError, match="not mounted"):
        paths.runs_dir()
    env.setenv("NABU_CHECKPOINT_DIR", "/c")  # an explicit root needs no drive check
    assert paths.checkpoint_dir() == Path("/c")


def test_expand(env, tmp_path):
    env.setenv("NABU_RUNS_DIR", str(tmp_path / "with space"))
    assert paths.expand("sqlite:///${NABU_RUNS_DIR}/mlflow.db") == (
        f"sqlite:///{tmp_path}/with space/mlflow.db"
    )
    with pytest.raises(ValueError, match="unknown variable"):
        paths.expand("${HOME}/x")


def test_config_paths(env):
    env.setenv("NABU_DATA_DIR", "/d")
    env.setenv("NABU_RUNS_DIR", "/r")
    env.setenv("NABU_CHECKPOINT_DIR", "/c")
    cfg = load_config(CONFIGS / "smoke.yaml")
    assert cfg.output_dir == "/r/smoke"
    assert cfg.checkpoint_dir == "/c/vits/smoke"
    assert cfg.data.manifests == ["/d/tts/smoke/manifest.jsonl"]
    assert cfg.mlflow.tracking_uri == "sqlite:////r/mlflow.db"
    # Without checkpoint_dir, checkpoints stay inside output_dir.
    cfg = config_from_dict({"output_dir": "out", "data": {"manifests": ["m.jsonl"]}})
    assert cfg.checkpoint_dir == "out/checkpoints"
