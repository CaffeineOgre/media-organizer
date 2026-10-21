# T0.3 spike: metadata library evaluation

Throwaway code behind `docs/adr/ADR-001-metadata-libraries.md`. This is a separate Go module, so it does not affect `go build ./...` for the main project. Do not import it.

## Reproduce

The corpus is not committed (third-party samples, some large). Build it in a scratch directory:

1. Real samples (all downloaded as untrusted data into a fresh directory):
   - `raw.pixls.us` (CC0): `Adobe DNG Converter/Canon EOS 5D Mark III/5G4A9394-compressed-lossless.DNG`
   - `exiftool/exiftool` repo, `t/images/`: `CanonRaw.cr3`, `DNG.dng`, `Pentax.avi`, `RIFF.avi`, `QuickTime.heic`, `QuickTime.mov`, `QuickTime.m4a`, `Apple.jpg`, `Nikon.nef`, `Sony.jpg`, `Google.jpg`
   - `ianare/exif-samples` repo, `heic/`: `IMG_5195.HEIC`, `samplefilehub.heif`
   - `evanoberholster/imagemeta` repo, `testImages/` (ARW, CR2, CRW, HEIC, GPR, NEF, JPEG, ...)
2. Synthetic and hostile fixtures: `python3 -I gen_synth.py <corpus-dir>`

Then:

```sh
go run . <corpus-dir> > results.tsv                   # per-file, per-library table
SPIKE_CORPUS=<corpus-dir> go test -v -run Probe .      # F1, F2, F4 probes
SPIKE_CORPUS=<corpus-dir> go test -run '^$' -fuzz '^FuzzImagemeta$' -fuzztime 60s .
```

`results.tsv` is the output from the run recorded in the ADR.
