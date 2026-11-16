from pathlib import Path

import pytest

from nabu_llm import serve, train

CONFIGS = Path(__file__).resolve().parents[1] / "configs"


@pytest.mark.parametrize("name", ["smoke.yaml", "full.yaml"])
def test_configs_expand(name, monkeypatch, tmp_path):
    monkeypatch.setenv("NABU_CHECKPOINT_DIR", str(tmp_path / "ck"))
    monkeypatch.setenv("NABU_RUNS_DIR", str(tmp_path / "runs"))
    cfg = train.load_config(CONFIGS / name)
    assert cfg["data"] == str(tmp_path / "ck" / "llm" / "data")
    m = train.mlx_config(cfg)
    assert m["mask_prompt"] is True and m["fine_tune_type"] == "lora"
    assert m["adapter_path"].startswith(str(tmp_path / "ck"))
    assert m["model"].startswith("mlx-community/Qwen3-4B-Instruct-2507")


def test_dry_run_and_missing_data(monkeypatch, tmp_path):
    monkeypatch.setenv("NABU_CHECKPOINT_DIR", str(tmp_path / "ck"))
    monkeypatch.setenv("NABU_RUNS_DIR", str(tmp_path / "runs"))
    monkeypatch.delenv("MODELS_ROOT", raising=False)
    with pytest.raises(FileNotFoundError, match="make llm-data"):
        train.main(["--config", str(CONFIGS / "smoke.yaml"), "--dry-run"])
    data = tmp_path / "ck" / "llm" / "data"
    data.mkdir(parents=True)
    for n in ("train.jsonl", "valid.jsonl"):
        (data / n).write_text('{"messages": []}\n')
    assert train.main(["--config", str(CONFIGS / "smoke.yaml"), "--dry-run"]) == 0
    assert (tmp_path / "runs" / "llm" / "smoke" / "mlx-lora.yaml").is_file()


def test_storage_guard(monkeypatch):
    monkeypatch.setenv("MODELS_ROOT", "/x")
    monkeypatch.delenv("HF_HOME", raising=False)
    with pytest.raises(RuntimeError, match="HF_HOME"):
        train.check_storage()


def test_serve_command(tmp_path):
    cfg = {"model": "m", "adapters": str(tmp_path)}
    assert "--adapter-path" not in serve.command(cfg, "127.0.0.1", 8090)
    (tmp_path / "adapters.safetensors").write_bytes(b"")
    assert serve.command(cfg, "127.0.0.1", 8090)[-2:] == ["--adapter-path", str(tmp_path)]
