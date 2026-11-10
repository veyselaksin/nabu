# TTS listening samples

Synthesized audio for comparing checkpoints by ear. Everything in this
directory except this README is git-ignored (no audio in git).

```sh
# after exporting a checkpoint (see tts/train/README.md):
make tts-samples MODEL=runs/full/model.onnx LABEL=step-200000
open eval/samples/index.html
```

Each run writes `eval/samples/<LABEL>/` with, per input in
`eval/testsets/tts-samples.tsv`:

- `<id>.wav`: the audio
- `<id>.txt`: transliteration, IPA, rule set + reference, model, speaker

plus `index.tsv` for the whole run. `index.html` puts every label side by
side, one column per checkpoint.

A failed input gets a `<id>.txt` starting with `ERROR:`, and the command
exits non-zero. Failures are not skipped silently.

TODO: phoneme error rate (phoneme recognizer → PER against the input IPA)
is not implemented yet.
