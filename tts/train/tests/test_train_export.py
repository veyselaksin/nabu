"""Tiny train → checkpoint → resume → ONNX export → parity check, on CPU."""

import json

import pytest

torch = pytest.importorskip("torch")
pytest.importorskip("onnxruntime")
pytest.importorskip("numba")

import yaml  # noqa: E402

from nabu_tts import export, smoke_data, train  # noqa: E402


def test_train_resume_export(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    assert smoke_data.main(["--out", "data", "--clips", "24", "--speakers", "2"]) == 0
    cfg = {
        "run_name": "pytest",
        "output_dir": "run",
        "data": {
            "manifests": ["data/manifest.jsonl"],
            "val_size": 2,
            "sample_rate": 16000,
            "filter_length": 256,
            "hop_length": 64,
            "win_length": 256,
            "n_mel_channels": 32,
        },
        "train": {
            "device": "cpu",
            "max_steps": 2,
            "batch_size": 4,
            "segment_size": 1024,
            "num_workers": 0,
            "log_every": 1,
            "eval_every": 2,
            "checkpoint_every": 1,
            "keep_checkpoints": 1,
            "bucket_boundaries": [100],
        },
        "model": {
            "inter_channels": 16,
            "hidden_channels": 16,
            "filter_channels": 32,
            "n_layers": 1,
            "resblock": "2",
            "resblock_kernel_sizes": [3],
            "resblock_dilation_sizes": [[1, 3]],
            "upsample_rates": [8, 8],
            "upsample_initial_channel": 16,
            "upsample_kernel_sizes": [16, 16],
            "gin_channels": 8,
        },
        "mlflow": {"tracking_uri": f"sqlite:///{tmp_path}/mlflow.db"},
    }
    (tmp_path / "c.yaml").write_text(yaml.safe_dump(cfg))
    assert train.main(["--config", "c.yaml"]) == 0
    ckpts = sorted((tmp_path / "run" / "checkpoints").glob("step_*.pt"))
    assert [p.name for p in ckpts] == ["step_00000002.pt"]  # keep_checkpoints: 1
    assert train.main(["--config", "c.yaml", "--fresh"]) == 2  # refuses to clobber

    # Resume continues from step 2 in the same MLflow run.
    assert train.main(["--config", "c.yaml", "--set", "train.max_steps=3"]) == 0
    ckpt = train.load_checkpoint(tmp_path / "run" / "checkpoints" / "latest.pt")
    assert ckpt["step"] == 3 and ckpt["speakers"] == ["synthetic-0", "synthetic-1"]

    assert export.main(["--checkpoint", "run/checkpoints/latest.pt", "--out", "m.onnx"]) == 0
    import onnx

    meta = {p.key: p.value for p in onnx.load("m.onnx").metadata_props}
    assert meta["nabu.format"] == "1"
    assert meta["nabu.symbols_sha256"] == ckpt["symbols_sha256"]
    assert json.loads(meta["nabu.speakers"]) == ["synthetic-0", "synthetic-1"]
    assert meta["nabu.sample_rate"] == "16000"
    card = (tmp_path / "m.model-card.md").read_text()
    assert "SYNTHETIC" in card and "${" not in card.replace("${...}", "")
