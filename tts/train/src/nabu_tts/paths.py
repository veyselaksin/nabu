"""Where heavy artifacts (datasets, checkpoints, training runs) live.

Each root comes from its own environment variable; without it, from
``$MODELS_ROOT/projects/nabu/<kind>`` when ``MODELS_ROOT`` is set (a shared
model drive), and otherwise from a git-ignored directory in the repository:

    NABU_DATA_DIR        $MODELS_ROOT/projects/nabu/datasets        data/corpora
    NABU_RUNS_DIR        $MODELS_ROOT/projects/nabu/training-runs   runs
    NABU_CHECKPOINT_DIR  $MODELS_ROOT/projects/nabu/checkpoints     runs/checkpoints

``NABU_HOME`` replaces ``$MODELS_ROOT/projects/nabu`` in the middle column.
Config files refer to the roots as ``${NABU_DATA_DIR}`` etc.; see ``expand``.
``nabu-tts-paths`` prints them (``nabu-tts-paths runs`` prints one).
"""

from __future__ import annotations

import os
import re
from pathlib import Path

_ROOTS = {
    "NABU_DATA_DIR": ("datasets", "data/corpora"),
    "NABU_RUNS_DIR": ("training-runs", "runs"),
    "NABU_CHECKPOINT_DIR": ("checkpoints", "runs/checkpoints"),
}
_VAR = re.compile(r"\$\{([A-Za-z_][A-Za-z0-9_]*)\}")


def nabu_home() -> str | None:
    """The shared artifact root, or None to use the repository's directories.

    Refuses a ``MODELS_ROOT`` that is not a directory (an unmounted drive):
    creating directories under its mount point would write to the internal disk.
    """
    if home := os.environ.get("NABU_HOME"):
        return home
    models_root = os.environ.get("MODELS_ROOT")
    if not models_root:
        return None
    if not os.path.isdir(models_root):
        raise RuntimeError(
            f"MODELS_ROOT={models_root!r} does not exist (drive not mounted?). "
            "Mount it, or unset MODELS_ROOT to use the repository's runs/ and data/."
        )
    return os.path.join(models_root, "projects", "nabu")


def root(name: str) -> str:
    """The directory for NABU_DATA_DIR, NABU_RUNS_DIR or NABU_CHECKPOINT_DIR."""
    sub, local = _ROOTS[name]
    if value := os.environ.get(name):
        return value
    home = nabu_home()
    return os.path.join(home, sub) if home else local


def data_dir() -> Path:
    return Path(root("NABU_DATA_DIR"))


def runs_dir() -> Path:
    return Path(root("NABU_RUNS_DIR"))


def checkpoint_dir() -> Path:
    return Path(root("NABU_CHECKPOINT_DIR"))


def expand(value: str) -> str:
    """Replace ${NABU_DATA_DIR}, ${NABU_RUNS_DIR} and ${NABU_CHECKPOINT_DIR}."""

    def sub(m: re.Match[str]) -> str:
        if m.group(1) not in _ROOTS:
            known = ", ".join(sorted(_ROOTS))
            raise ValueError(f"{value!r}: unknown variable ${{{m.group(1)}}} (use {known})")
        return root(m.group(1))

    return _VAR.sub(sub, value)


def main(argv: list[str] | None = None) -> int:
    import argparse

    kinds = {"data": "NABU_DATA_DIR", "runs": "NABU_RUNS_DIR", "checkpoints": "NABU_CHECKPOINT_DIR"}
    ap = argparse.ArgumentParser(prog="nabu-tts-paths", description=__doc__.split("\n")[0])
    ap.add_argument("kind", nargs="?", choices=sorted(kinds), help="print only this root")
    args = ap.parse_args(argv)
    if args.kind:
        print(os.path.abspath(root(kinds[args.kind])))
    else:
        for name in kinds.values():
            print(f"{name}={os.path.abspath(root(name))}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
