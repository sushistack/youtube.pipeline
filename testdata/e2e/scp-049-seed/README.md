# SCP-049 Canonical E2E Seed

Bundled fixture for Go pipeline E2E tests (SMOKE-02/04/07/08 today; SMOKE-01/03/05/06 after Step 5/6 backfill). Single source of truth for "a small but realistic Phase A output that Phase B and Phase C can consume", so future smokes diff against the same bytes.

Layout:
- `raw.txt` — SCP-049 corpus stub (~1 KB). Phase A agents are mocked in tests; the corpus is here only so the seed is self-contained.
- `scenario.json` — minimal `agents.PipelineState` (RunID, SCPID, timestamps). Tests pass scenarios by-value to phase_b; the file exists so future SMOKEs that exercise `PhaseBRequest.ScenarioPath` can read it.
- `responses/images/scene_{00,01,02}.png` — 1920×1080 solid red/green/blue, ~9 KB each.
- `responses/tts/scene_{00,01,02}.wav` — 1.0 s mono silence, 22050 Hz, ~44 KB each.
- `expected-manifest.json` — golden assertions (scene count, codec, duration tolerance).

Total bundle size ~200 KB.

## Regenerating fixtures

PNGs and WAVs are deterministic ffmpeg lavfi outputs. To rebuild:

```sh
cd testdata/e2e/scp-049-seed
for i in 0:red 1:green 2:blue; do
  idx=${i%:*}; color=${i#*:}
  ffmpeg -y -loglevel error -f lavfi -i "color=c=$color:s=1920x1080:d=1" \
    -frames:v 1 responses/images/scene_0$idx.png
  ffmpeg -y -loglevel error -f lavfi -i "anullsrc=channel_layout=mono:sample_rate=22050" \
    -t 1.0 responses/tts/scene_0$idx.wav
done
```

Bytes may shift across ffmpeg versions; if a regeneration breaks SMOKE byte-identity assertions (none today, but SMOKE-08 may add one), pin the regeneration commit and update the assertion.
