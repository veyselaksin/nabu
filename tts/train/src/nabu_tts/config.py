"""Training configuration, loaded from YAML (see tts/train/configs/)."""

from __future__ import annotations

import dataclasses
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

import yaml


@dataclass
class DataConfig:
    # JSONL manifests written by nabu-tts-prepare or nabu-tts-smoke-data.
    manifests: list[str] = field(default_factory=list)
    # Clips held out for validation, chosen deterministically by hashing audio_path.
    val_size: int = 50
    sample_rate: int = 22050
    filter_length: int = 1024
    hop_length: int = 256
    win_length: int = 1024
    n_mel_channels: int = 80
    mel_fmin: float = 0.0
    mel_fmax: float | None = None
    add_blank: bool = True
    # Clips whose token sequence (before add_blank) is outside this range are skipped.
    min_tokens: int = 1
    max_tokens: int = 300
    # Optional: only use these languages from the manifests (empty = all).
    languages: list[str] = field(default_factory=list)


@dataclass
class TrainConfig:
    device: str = "auto"  # auto | cpu | cuda | mps
    seed: int = 1234
    max_steps: int = 500_000
    batch_size: int = 32
    learning_rate: float = 2e-4
    betas: tuple[float, float] = (0.8, 0.99)
    eps: float = 1e-9
    lr_decay: float = 0.999875  # applied once per epoch, as in upstream VITS
    segment_size: int = 8192  # samples; must be a multiple of hop_length
    c_mel: float = 45.0
    c_kl: float = 1.0
    fp16: bool = False  # CUDA only
    num_workers: int = 2
    log_every: int = 50
    eval_every: int = 2000
    checkpoint_every: int = 2000
    keep_checkpoints: int = 5
    # Speaker-balanced length bucketing (upstream's DistributedBucketSampler).
    bucket_boundaries: list[int] = field(
        default_factory=lambda: [32, 300, 400, 500, 600, 700, 800, 900, 1000]
    )


@dataclass
class ModelConfig:
    inter_channels: int = 192
    hidden_channels: int = 192
    filter_channels: int = 768
    n_heads: int = 2
    n_layers: int = 6
    kernel_size: int = 3
    p_dropout: float = 0.1
    resblock: str = "1"
    resblock_kernel_sizes: list[int] = field(default_factory=lambda: [3, 7, 11])
    resblock_dilation_sizes: list[list[int]] = field(
        default_factory=lambda: [[1, 3, 5], [1, 3, 5], [1, 3, 5]]
    )
    upsample_rates: list[int] = field(default_factory=lambda: [8, 8, 2, 2])
    upsample_initial_channel: int = 512
    upsample_kernel_sizes: list[int] = field(default_factory=lambda: [16, 16, 4, 4])
    gin_channels: int = 256
    use_sdp: bool = True
    use_spectral_norm: bool = False


@dataclass
class MlflowConfig:
    # sqlite store via mlflow-skinny + sqlalchemy. View with:
    #   uvx mlflow ui --backend-store-uri sqlite:///runs/mlflow.db
    tracking_uri: str = "sqlite:///runs/mlflow.db"
    experiment: str = "nabu-tts"
    enabled: bool = True


@dataclass
class Config:
    run_name: str = "nabu-tts"
    output_dir: str = "runs/nabu-tts"
    symbols: str | None = None  # default: tts/symbols.txt
    data: DataConfig = field(default_factory=DataConfig)
    train: TrainConfig = field(default_factory=TrainConfig)
    model: ModelConfig = field(default_factory=ModelConfig)
    mlflow: MlflowConfig = field(default_factory=MlflowConfig)

    def to_dict(self) -> dict[str, Any]:
        return dataclasses.asdict(self)

    def validate(self) -> None:
        hop = self.data.hop_length
        if self.train.segment_size % hop:
            raise ValueError(f"train.segment_size must be a multiple of data.hop_length ({hop})")
        total = 1
        for r in self.model.upsample_rates:
            total *= r
        if total != hop:
            raise ValueError(
                f"product of model.upsample_rates ({total}) must equal hop_length ({hop})"
            )
        if not self.data.manifests:
            raise ValueError("data.manifests must list at least one manifest")


def _build(cls: type, raw: dict[str, Any] | None, where: str) -> Any:
    raw = raw or {}
    names = {f.name: f for f in dataclasses.fields(cls)}
    unknown = sorted(set(raw) - set(names))
    if unknown:
        raise ValueError(f"{where}: unknown keys {unknown}")
    kwargs = {}
    for k, v in raw.items():
        sub = _SECTIONS.get(k) if cls is Config else None
        if sub:
            v = _build(sub, v, f"{where}.{k}")
        elif names[k].type in ("float", "float | None") and isinstance(v, (int, str)):
            # PyYAML reads "2e-4" (no dot) as a string; accept it as a float.
            try:
                v = float(v)
            except ValueError:
                raise ValueError(f"{where}.{k}: expected a number, got {v!r}") from None
        kwargs[k] = v
    if cls is TrainConfig and "betas" in kwargs:
        kwargs["betas"] = tuple(kwargs["betas"])
    return cls(**kwargs)


_SECTIONS: dict[str, type] = {
    "data": DataConfig,
    "train": TrainConfig,
    "model": ModelConfig,
    "mlflow": MlflowConfig,
}


def config_from_dict(raw: dict[str, Any], where: str = "config") -> Config:
    cfg = _build(Config, raw, where)
    cfg.validate()
    return cfg


def load_config(path: str | Path) -> Config:
    with open(path, encoding="utf-8") as f:
        raw = yaml.safe_load(f) or {}
    return config_from_dict(raw, str(path))
