# nabu-tts

**Status: stub.** `synthesize` raises `NotImplementedError`.

Planned role: take the IPA string produced by the G2P engine and return
spoken audio as a WAV file (`bytes`). The API's `POST /v1/speak` endpoint and
the web demo's "Speak" button will be wired to it.

```python
from nabu_tts import synthesize

wav: bytes = synthesize("…", voice="default")
```

Constraints for whichever engine is chosen:

- The code and any voice or model weights must be under an OSI-approved,
  commercially usable license (Apache-2.0, MIT, BSD, MPL). That rules out GPL
  engines and voices with non-commercial or custom licenses.
- It must handle the full IPA inventory the rule files emit (e.g. pharyngeals
  and uvulars), not only the phonemes of modern languages.
- It must run on CPU.
