"""nabu-tts-train: train the multi-speaker, IPA-input VITS model.

    uv run nabu-tts-train --config tts/train/configs/smoke.yaml
    uv run nabu-tts-train --config tts/train/configs/full.yaml   # resumes automatically

A run lives in ``output_dir``: checkpoints/step_XXXXXXXX.pt, checkpoints/latest.pt,
config.yaml, samples/. If ``checkpoints/latest.pt`` exists the run resumes from it
(same MLflow run); pass --fresh to refuse to resume. Training stops at
``train.max_steps``; Ctrl-C saves a checkpoint first.
"""

from __future__ import annotations

import argparse
import contextlib
import math
import os
import random
import shutil
import sys
import time
from pathlib import Path
from typing import Any

import numpy as np
import torch
import yaml
from torch.nn import functional as F

from nabu_tts.audio import write_wav
from nabu_tts.config import Config, config_from_dict, load_config
from nabu_tts.dataset import BucketBatchSampler, Corpus, VitsDataset, collate, load_corpus
from nabu_tts.symbols import SymbolTable, load_symbols
from nabu_tts.vits import commons
from nabu_tts.vits.losses import discriminator_loss, feature_loss, generator_loss, kl_loss
from nabu_tts.vits.mel import mel_spectrogram_torch, spec_to_mel_torch
from nabu_tts.vits.models import MultiPeriodDiscriminator, SynthesizerTrn

CHECKPOINT_FORMAT = 1


def build_generator(cfg: Config, n_vocab: int, n_speakers: int) -> SynthesizerTrn:
    m = cfg.model
    return SynthesizerTrn(
        n_vocab,
        cfg.data.filter_length // 2 + 1,
        cfg.train.segment_size // cfg.data.hop_length,
        inter_channels=m.inter_channels,
        hidden_channels=m.hidden_channels,
        filter_channels=m.filter_channels,
        n_heads=m.n_heads,
        n_layers=m.n_layers,
        kernel_size=m.kernel_size,
        p_dropout=m.p_dropout,
        resblock=m.resblock,
        resblock_kernel_sizes=m.resblock_kernel_sizes,
        resblock_dilation_sizes=m.resblock_dilation_sizes,
        upsample_rates=m.upsample_rates,
        upsample_initial_channel=m.upsample_initial_channel,
        upsample_kernel_sizes=m.upsample_kernel_sizes,
        n_speakers=n_speakers,
        gin_channels=m.gin_channels,
        use_sdp=m.use_sdp,
    )


def pick_device(name: str) -> torch.device:
    if name == "auto":
        return torch.device("cuda" if torch.cuda.is_available() else "cpu")
    return torch.device(name)


def seed_everything(seed: int) -> None:
    random.seed(seed)
    np.random.seed(seed)
    torch.manual_seed(seed)


# ---------------------------------------------------------------- checkpoints


def save_checkpoint(path: Path, state: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(".tmp")
    torch.save(state, tmp)
    os.replace(tmp, path)


def load_checkpoint(path: str | Path, map_location: str | torch.device = "cpu") -> dict[str, Any]:
    ckpt = torch.load(path, map_location=map_location, weights_only=False)
    if ckpt.get("format") != CHECKPOINT_FORMAT:
        raise ValueError(f"{path}: unsupported checkpoint format {ckpt.get('format')!r}")
    return ckpt


def prune_checkpoints(ckpt_dir: Path, keep: int) -> None:
    steps = sorted(ckpt_dir.glob("step_*.pt"))
    for old in steps[: max(0, len(steps) - keep)]:
        old.unlink()


# ---------------------------------------------------------------- mlflow


class Tracker:
    """Thin MLflow wrapper; a no-op when disabled."""

    def __init__(self, cfg: Config, run_id: str | None) -> None:
        self.enabled = cfg.mlflow.enabled
        self.run_id = run_id
        if not self.enabled:
            return
        os.environ.setdefault("MLFLOW_DISABLE_AGENT_HINT", "1")
        import mlflow

        self.mlflow = mlflow
        uri = cfg.mlflow.tracking_uri
        if uri.startswith("sqlite:///"):
            Path(uri.removeprefix("sqlite:///")).parent.mkdir(parents=True, exist_ok=True)
        mlflow.set_tracking_uri(uri)
        mlflow.set_experiment(cfg.mlflow.experiment)
        run = mlflow.start_run(run_id=run_id, run_name=None if run_id else cfg.run_name)
        self.run_id = run.info.run_id

    def params(self, params: dict[str, Any]) -> None:
        if self.enabled:
            flat = _flatten(params)
            # MLflow params are immutable; on resume only new keys are logged.
            existing = self.mlflow.get_run(self.run_id).data.params
            new = {k: v for k, v in flat.items() if k not in existing}
            for i in range(0, len(new), 100):
                self.mlflow.log_params(dict(list(new.items())[i : i + 100]))

    def metrics(self, values: dict[str, float], step: int) -> None:
        if self.enabled:
            self.mlflow.log_metrics(values, step=step)

    def artifacts(self, path: Path, artifact_path: str) -> None:
        if self.enabled:
            self.mlflow.log_artifacts(str(path), artifact_path=artifact_path)

    def end(self) -> None:
        if self.enabled:
            self.mlflow.end_run()


def _flatten(d: dict[str, Any], prefix: str = "") -> dict[str, str]:
    out: dict[str, str] = {}
    for k, v in d.items():
        key = f"{prefix}{k}"
        if isinstance(v, dict):
            out.update(_flatten(v, key + "."))
        else:
            out[key] = str(v)[:500]
    return out


# ---------------------------------------------------------------- evaluation


@torch.no_grad()
def evaluate(
    net_g: SynthesizerTrn,
    loader: torch.utils.data.DataLoader,
    cfg: Config,
    device: torch.device,
    sample_dir: Path,
    max_samples: int = 4,
) -> dict[str, float]:
    """Reconstruction mel L1 on the validation set, plus a few synthesized samples."""
    net_g.eval()
    d = cfg.data
    total, n = 0.0, 0
    sample_dir.mkdir(parents=True, exist_ok=True)
    written = 0
    for x, x_len, spec, spec_len, y, _y_len, sid in loader:
        x, x_len, spec, spec_len, sid = (t.to(device) for t in (x, x_len, spec, spec_len, sid))
        g = net_g.emb_g(sid).unsqueeze(-1)
        z, _m_q, _logs_q, y_mask = net_g.enc_q(spec, spec_len, g=g)
        y_rec = net_g.dec(z * y_mask, g=g).squeeze(1)
        mel = spec_to_mel_torch(
            spec, d.filter_length, d.n_mel_channels, d.sample_rate, d.mel_fmin, d.mel_fmax
        )
        mel_rec = mel_spectrogram_torch(
            y_rec,
            d.filter_length,
            d.n_mel_channels,
            d.sample_rate,
            d.hop_length,
            d.win_length,
            d.mel_fmin,
            d.mel_fmax,
        )
        frames = min(mel.size(2), mel_rec.size(2))
        total += F.l1_loss(mel[:, :, :frames], mel_rec[:, :, :frames]).item() * x.size(0)
        n += x.size(0)
        for i in range(x.size(0)):
            if written >= max_samples:
                break
            o = net_g.infer(
                x[i : i + 1, : x_len[i]],
                x_len[i : i + 1],
                sid=sid[i : i + 1],
                noise_scale=0.667,
                noise_scale_w=0.8,
                length_scale=1.0,
            )[0]
            write_wav(
                sample_dir / f"val_{written}.wav", o[0, 0].float().cpu().numpy(), d.sample_rate
            )
            write_wav(sample_dir / f"val_{written}_ref.wav", y[i, 0].cpu().numpy(), d.sample_rate)
            written += 1
    net_g.train()
    return {"val/mel_l1_reconstruction": total / max(1, n)}


# ---------------------------------------------------------------- training


def train(cfg: Config, symbols: SymbolTable, fresh: bool) -> int:
    out = Path(cfg.output_dir)
    ckpt_dir = out / "checkpoints"
    latest = ckpt_dir / "latest.pt"
    resume = latest.exists()
    if resume and fresh:
        print(f"{latest} exists and --fresh was given; use a new output_dir", file=sys.stderr)
        return 2
    out.mkdir(parents=True, exist_ok=True)
    seed_everything(cfg.train.seed)
    device = pick_device(cfg.train.device)

    ckpt = load_checkpoint(latest) if resume else None
    if ckpt and ckpt["symbols_sha256"] != symbols.sha256():
        print(
            f"{latest} was trained with a different symbol table "
            f"({ckpt['symbols_sha256'][:12]} ≠ {symbols.sha256()[:12]}). Start a new run.",
            file=sys.stderr,
        )
        return 2

    seg_frames = cfg.train.segment_size // cfg.data.hop_length
    corpus: Corpus = load_corpus(cfg.data, symbols, min_frames=seg_frames + 2)
    if corpus.stats["skipped"].get("unknown_symbol"):
        print(
            f"{corpus.stats['skipped']['unknown_symbol']} manifest entries contain IPA symbols "
            "missing from the symbol table. Re-run nabu-tts-prepare with the current table.",
            file=sys.stderr,
        )
        return 2
    if ckpt and ckpt["speakers"] != corpus.speakers:
        print(
            f"{latest}: the manifests' speaker set changed since this run started.", file=sys.stderr
        )
        return 2
    if len(corpus.train) < cfg.train.batch_size:
        print(f"only {len(corpus.train)} training clips; need at least batch_size", file=sys.stderr)
        return 2
    print(
        f"data: {corpus.stats['clips_train']} train / {corpus.stats['clips_val']} val clips, "
        f"{len(corpus.speakers)} speakers, languages {corpus.stats['languages']}, "
        f"skipped {corpus.stats['skipped']}"
    )

    net_g = build_generator(cfg, len(symbols), len(corpus.speakers)).to(device)
    net_d = MultiPeriodDiscriminator(cfg.model.use_spectral_norm).to(device)
    t = cfg.train
    optim_g = torch.optim.AdamW(net_g.parameters(), t.learning_rate, betas=t.betas, eps=t.eps)
    optim_d = torch.optim.AdamW(net_d.parameters(), t.learning_rate, betas=t.betas, eps=t.eps)
    sched_g = torch.optim.lr_scheduler.ExponentialLR(optim_g, gamma=t.lr_decay)
    sched_d = torch.optim.lr_scheduler.ExponentialLR(optim_d, gamma=t.lr_decay)
    use_amp = t.fp16 and device.type == "cuda"
    scaler = torch.amp.GradScaler("cuda", enabled=use_amp)

    step, epoch, run_id = 0, 0, None
    if ckpt:
        net_g.load_state_dict(ckpt["net_g"])
        net_d.load_state_dict(ckpt["net_d"])
        optim_g.load_state_dict(ckpt["optim_g"])
        optim_d.load_state_dict(ckpt["optim_d"])
        sched_g.load_state_dict(ckpt["sched_g"])
        sched_d.load_state_dict(ckpt["sched_d"])
        scaler.load_state_dict(ckpt["scaler"])
        step, epoch, run_id = ckpt["step"], ckpt["epoch"], ckpt.get("mlflow_run_id")
        random.setstate(ckpt["rng"]["python"])
        np.random.set_state(ckpt["rng"]["numpy"])
        torch.set_rng_state(ckpt["rng"]["torch"])
        print(f"resumed from {latest} at step {step} (epoch {epoch})")

    (out / "config.yaml").write_text(
        yaml.safe_dump(cfg.to_dict(), sort_keys=False, allow_unicode=True)
    )
    tracker = Tracker(cfg, run_id)
    tracker.params(
        {
            "config": cfg.to_dict(),
            "symbols_sha256": symbols.sha256(),
            "n_vocab": len(symbols),
            "n_speakers": len(corpus.speakers),
            "data": corpus.stats,
        }
    )

    sampler = BucketBatchSampler(
        [it.frames for it in corpus.train], t.batch_size, t.bucket_boundaries, t.seed
    )
    loader = torch.utils.data.DataLoader(
        VitsDataset(corpus.train, cfg.data),
        batch_sampler=sampler,
        collate_fn=collate,
        num_workers=t.num_workers,
        pin_memory=device.type == "cuda",
        persistent_workers=t.num_workers > 0,
    )
    val_loader = (
        torch.utils.data.DataLoader(
            VitsDataset(corpus.val, cfg.data), batch_size=t.batch_size, collate_fn=collate
        )
        if corpus.val
        else None
    )

    def state() -> dict[str, Any]:
        return {
            "format": CHECKPOINT_FORMAT,
            "step": step,
            "epoch": epoch,
            "config": cfg.to_dict(),
            "symbols": list(symbols.symbols),
            "symbols_sha256": symbols.sha256(),
            "speakers": corpus.speakers,
            "data_stats": corpus.stats,
            "mlflow_run_id": tracker.run_id,
            "net_g": net_g.state_dict(),
            "net_d": net_d.state_dict(),
            "optim_g": optim_g.state_dict(),
            "optim_d": optim_d.state_dict(),
            "sched_g": sched_g.state_dict(),
            "sched_d": sched_d.state_dict(),
            "scaler": scaler.state_dict(),
            "rng": {
                "python": random.getstate(),
                "numpy": np.random.get_state(),
                "torch": torch.get_rng_state(),
            },
        }

    def checkpoint() -> None:
        s = state()
        save_checkpoint(ckpt_dir / f"step_{step:08d}.pt", s)
        shutil.copyfile(ckpt_dir / f"step_{step:08d}.pt", ckpt_dir / "latest.pt.tmp")
        os.replace(ckpt_dir / "latest.pt.tmp", latest)
        prune_checkpoints(ckpt_dir, t.keep_checkpoints)
        print(f"checkpoint: {ckpt_dir / f'step_{step:08d}.pt'}")

    d = cfg.data
    net_g.train()
    net_d.train()
    autocast = (
        (lambda: torch.autocast("cuda", dtype=torch.float16)) if use_amp else contextlib.nullcontext
    )
    t0, start_step = time.time(), step
    try:
        while step < t.max_steps:
            sampler.set_epoch(epoch)
            for x, x_len, spec, spec_len, y, _y_len, sid in loader:
                x, x_len, spec, spec_len, y, sid = (
                    v.to(device, non_blocking=True) for v in (x, x_len, spec, spec_len, y, sid)
                )
                with autocast():
                    (
                        y_hat,
                        l_length,
                        _attn,
                        ids_slice,
                        _x_mask,
                        z_mask,
                        (
                            _z,
                            z_p,
                            m_p,
                            logs_p,
                            _m_q,
                            logs_q,
                        ),
                    ) = net_g(x, x_len, spec, spec_len, sid)
                    mel = spec_to_mel_torch(
                        spec,
                        d.filter_length,
                        d.n_mel_channels,
                        d.sample_rate,
                        d.mel_fmin,
                        d.mel_fmax,
                    )
                    y_mel = commons.slice_segments(mel, ids_slice, seg_frames)
                    y_hat_mel = mel_spectrogram_torch(
                        y_hat.squeeze(1).float(),
                        d.filter_length,
                        d.n_mel_channels,
                        d.sample_rate,
                        d.hop_length,
                        d.win_length,
                        d.mel_fmin,
                        d.mel_fmax,
                    )
                    y_seg = commons.slice_segments(y, ids_slice * d.hop_length, t.segment_size)
                    y_d_hat_r, y_d_hat_g, _, _ = net_d(y_seg, y_hat.detach())
                    loss_disc, _, _ = discriminator_loss(y_d_hat_r, y_d_hat_g)
                optim_d.zero_grad()
                scaler.scale(loss_disc).backward()
                scaler.unscale_(optim_d)
                grad_norm_d = commons.clip_grad_value_(net_d.parameters(), None)
                scaler.step(optim_d)

                with autocast():
                    y_d_hat_r, y_d_hat_g, fmap_r, fmap_g = net_d(y_seg, y_hat)
                    loss_dur = torch.sum(l_length.float())
                    loss_mel = F.l1_loss(y_mel, y_hat_mel) * t.c_mel
                    loss_kl = kl_loss(z_p, logs_q, m_p, logs_p, z_mask) * t.c_kl
                    loss_fm = feature_loss(fmap_r, fmap_g)
                    loss_gen, _ = generator_loss(y_d_hat_g)
                    loss_gen_all = loss_gen + loss_fm + loss_mel + loss_dur + loss_kl
                optim_g.zero_grad()
                scaler.scale(loss_gen_all).backward()
                scaler.unscale_(optim_g)
                grad_norm_g = commons.clip_grad_value_(net_g.parameters(), None)
                scaler.step(optim_g)
                scaler.update()
                step += 1

                if not math.isfinite(loss_gen_all.item()):
                    print(f"step {step}: non-finite loss; stopping without saving", file=sys.stderr)
                    return 1
                if step % t.log_every == 0 or step == 1:
                    m = {
                        "loss/g_total": loss_gen_all.item(),
                        "loss/d_total": loss_disc.item(),
                        "loss/g_adv": loss_gen.item(),
                        "loss/fm": loss_fm.item(),
                        "loss/mel": loss_mel.item(),
                        "loss/dur": loss_dur.item(),
                        "loss/kl": loss_kl.item(),
                        "grad_norm/g": float(grad_norm_g),
                        "grad_norm/d": float(grad_norm_d),
                        "lr": optim_g.param_groups[0]["lr"],
                        "epoch": epoch,
                    }
                    tracker.metrics(m, step)
                    rate = (step - start_step) / max(1e-9, time.time() - t0)
                    print(
                        f"step {step} epoch {epoch} "
                        f"g={m['loss/g_total']:.3f} d={m['loss/d_total']:.3f} "
                        f"mel={m['loss/mel']:.3f} dur={m['loss/dur']:.3f} kl={m['loss/kl']:.3f}"
                        + f" ({rate:.2f} steps/s)"
                    )
                if val_loader is not None and step % t.eval_every == 0:
                    sample_dir = out / "samples" / f"step_{step:08d}"
                    vm = evaluate(net_g, val_loader, cfg, device, sample_dir)
                    tracker.metrics(vm, step)
                    tracker.artifacts(sample_dir, f"samples/step_{step:08d}")
                    print(f"eval step {step}: {vm}")
                if step % t.checkpoint_every == 0:
                    checkpoint()
                if step >= t.max_steps:
                    break
            epoch += 1
            sched_g.step()
            sched_d.step()
    except KeyboardInterrupt:
        print("interrupted; saving checkpoint", file=sys.stderr)
        checkpoint()
        tracker.end()
        return 130
    if not (ckpt_dir / f"step_{step:08d}.pt").exists():
        checkpoint()
    tracker.end()
    print(f"done: step {step} ({time.time() - t0:.0f}s this session); latest checkpoint {latest}")
    return 0


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(prog="nabu-tts-train", description=__doc__.split("\n")[0])
    ap.add_argument("--config", required=True)
    ap.add_argument("--fresh", action="store_true", help="fail instead of resuming an existing run")
    ap.add_argument(
        "--set",
        action="append",
        default=[],
        metavar="KEY=VALUE",
        help="override a config value, e.g. --set train.max_steps=100 (YAML value syntax)",
    )
    args = ap.parse_args(argv)
    cfg = load_config(args.config)
    if args.set:
        raw = cfg.to_dict()
        for kv in args.set:
            key, _, value = kv.partition("=")
            node = raw
            *parents, leaf = key.split(".")
            for p in parents:
                node = node[p]
            if leaf not in node:
                ap.error(f"--set {key}: unknown key")
            node[leaf] = yaml.safe_load(value)
        cfg = config_from_dict(raw, "config (with --set)")
    symbols = load_symbols(cfg.symbols)
    return train(cfg, symbols, args.fresh)


if __name__ == "__main__":
    raise SystemExit(main())
