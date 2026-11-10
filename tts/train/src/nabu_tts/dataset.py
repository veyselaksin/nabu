"""Manifest-backed dataset, collation and length-bucketed batching for VITS."""

from __future__ import annotations

import hashlib
import random
from collections import Counter
from collections.abc import Iterator
from dataclasses import dataclass
from pathlib import Path

import torch

from nabu_tts.audio import read_wav
from nabu_tts.config import DataConfig
from nabu_tts.manifest import Entry, read_manifest
from nabu_tts.symbols import SymbolTable, UnknownSymbolError
from nabu_tts.vits.mel import spectrogram_torch


@dataclass(frozen=True)
class Item:
    wav: Path
    tokens: list[int]
    speaker: int
    frames: int  # approximate spectrogram length, for bucketing
    entry: Entry


@dataclass
class Corpus:
    train: list[Item]
    val: list[Item]
    speakers: list[str]  # index = speaker id in the model
    stats: dict  # for the model card and logging


def _hash(s: str) -> str:
    return hashlib.sha256(s.encode()).hexdigest()


def load_corpus(cfg: DataConfig, symbols: SymbolTable, min_frames: int = 0) -> Corpus:
    """Read manifests, tokenize, assign speaker ids and split train/val deterministically.

    Clips shorter than ``min_frames`` spectrogram frames (the training segment)
    are skipped, since VITS slices a fixed-size segment from every clip.
    """
    entries: list[tuple[Entry, Path]] = []
    for m in cfg.manifests:
        entries.extend(read_manifest(m))
    skipped: Counter[str] = Counter()
    if cfg.languages:
        keep = set(cfg.languages)
        skipped["language_not_selected"] = sum(1 for e, _ in entries if e.language not in keep)
        entries = [(e, p) for e, p in entries if e.language in keep]

    speakers = sorted({e.speaker_id for e, _ in entries})
    spk_index = {s: i for i, s in enumerate(speakers)}
    items: list[Item] = []
    for e, path in entries:
        try:
            ids = symbols.tokenize(e.ipa)
        except UnknownSymbolError:
            # The symbol table changed since the manifest was written. Count it; the
            # caller fails if this is not zero (see train.py).
            skipped["unknown_symbol"] += 1
            continue
        if not (cfg.min_tokens <= len(ids) <= cfg.max_tokens):
            skipped["token_length"] += 1
            continue
        if cfg.add_blank:
            ids = symbols.intersperse(ids)
        frames = int(e.duration * cfg.sample_rate / cfg.hop_length)
        if frames < min_frames:
            skipped["shorter_than_segment"] += 1
            continue
        items.append(Item(path, ids, spk_index[e.speaker_id], frames, e))

    items.sort(key=lambda it: _hash(it.entry.audio_path + "|" + it.entry.speaker_id))
    val_n = min(cfg.val_size, max(0, len(items) // 10))
    val, train = items[:val_n], items[val_n:]

    by_lang: dict[str, dict] = {}
    for it in items:
        d = by_lang.setdefault(it.entry.language, {"clips": 0, "hours": 0.0, "speakers": set()})
        d["clips"] += 1
        d["hours"] += it.entry.duration / 3600
        d["speakers"].add(it.entry.speaker_id)
    stats = {
        "manifests": list(cfg.manifests),
        "clips_train": len(train),
        "clips_val": len(val),
        "skipped": dict(skipped),
        "languages": {
            k: {"clips": v["clips"], "hours": round(v["hours"], 3), "speakers": len(v["speakers"])}
            for k, v in sorted(by_lang.items())
        },
    }
    return Corpus(train=train, val=val, speakers=speakers, stats=stats)


class VitsDataset(torch.utils.data.Dataset):
    def __init__(self, items: list[Item], cfg: DataConfig) -> None:
        self.items = items
        self.cfg = cfg

    def __len__(self) -> int:
        return len(self.items)

    def __getitem__(self, i: int) -> tuple[torch.Tensor, torch.Tensor, torch.Tensor, int]:
        it = self.items[i]
        x, sr = read_wav(it.wav)
        if sr != self.cfg.sample_rate:
            raise ValueError(f"{it.wav}: sample rate {sr}, config expects {self.cfg.sample_rate}")
        wav = torch.from_numpy(x).unsqueeze(0)  # [1, T]
        c = self.cfg
        spec = spectrogram_torch(wav, c.filter_length, c.sample_rate, c.hop_length, c.win_length)
        return torch.LongTensor(it.tokens), spec.squeeze(0), wav, it.speaker


def collate(batch: list) -> tuple[torch.Tensor, ...]:
    """Pad a batch, longest spectrogram first (as upstream VITS)."""
    order = sorted(range(len(batch)), key=lambda i: batch[i][1].size(1), reverse=True)
    max_text = max(b[0].size(0) for b in batch)
    max_spec = max(b[1].size(1) for b in batch)
    max_wav = max(b[2].size(1) for b in batch)
    n = len(batch)
    text = torch.zeros(n, max_text, dtype=torch.long)
    spec = torch.zeros(n, batch[0][1].size(0), max_spec)
    wav = torch.zeros(n, 1, max_wav)
    text_len = torch.zeros(n, dtype=torch.long)
    spec_len = torch.zeros(n, dtype=torch.long)
    wav_len = torch.zeros(n, dtype=torch.long)
    sid = torch.zeros(n, dtype=torch.long)
    for j, i in enumerate(order):
        t, s, w, spk = batch[i]
        text[j, : t.size(0)] = t
        text_len[j] = t.size(0)
        spec[j, :, : s.size(1)] = s
        spec_len[j] = s.size(1)
        wav[j, :, : w.size(1)] = w
        wav_len[j] = w.size(1)
        sid[j] = spk
    return text, text_len, spec, spec_len, wav, wav_len, sid


class BucketBatchSampler(torch.utils.data.Sampler[list[int]]):
    """Batches of similar length (fewer padded frames), reshuffled every epoch."""

    def __init__(
        self, frames: list[int], batch_size: int, boundaries: list[int], seed: int
    ) -> None:
        self.batch_size = batch_size
        self.seed = seed
        self.epoch = 0
        self.buckets: list[list[int]] = [[] for _ in range(len(boundaries) + 1)]
        for i, f in enumerate(frames):
            b = sum(1 for edge in boundaries if f > edge)
            self.buckets[b].append(i)
        self.buckets = [b for b in self.buckets if b]

    def set_epoch(self, epoch: int) -> None:
        self.epoch = epoch

    def _batches(self) -> list[list[int]]:
        rng = random.Random(self.seed + self.epoch)
        batches = []
        for bucket in self.buckets:
            idx = bucket[:]
            rng.shuffle(idx)
            batches.extend(
                idx[i : i + self.batch_size] for i in range(0, len(idx), self.batch_size)
            )
        rng.shuffle(batches)
        return batches

    def __iter__(self) -> Iterator[list[int]]:
        return iter(self._batches())

    def __len__(self) -> int:
        return sum((len(b) + self.batch_size - 1) // self.batch_size for b in self.buckets)
