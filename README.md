# media-organizer

A pure-Go CLI tool that scans folders of photos and movies, reads their embedded metadata (EXIF, QuickTime/MP4 atoms, etc.), and copies, or optionally moves them into a configurable folder tree with optional renaming.

## Features

- **Pure Go**: Single static binary, no cgo, no external dependencies (`exiftool`/`ffprobe`).
- **Safe by Default**: Copy mode by default; move requires an explicit flag. No overwriting of existing files (`O_EXCL`).
- **Date Source Fallback**: Configurable date chain (default: `exif` → `filename` → `mtime`). Files dated from `mtime` are routed to an `Unverified/` subtree.
- **Deduplication & Collision Handling**: Built-in streaming SHA-256 duplicate detection with configurable collision policies.
- **Reversible Operations**: Transactional execution with append-only JSONL journal allowing complete `undo`.
- **Cross-Platform**: Safe path sanitization and support for Linux, macOS, and Windows.

## Repository Layout

```
.
├── cmd/
│   └── media-organizer/ # Main CLI binary entrypoint
├── internal/
│   ├── cli/             # CLI command routing, flags, prompts, and reports
│   ├── config/          # TOML configuration loading and schema validation
│   ├── datechain/       # Fallback date extraction and timezone resolution
│   ├── detect/          # Magic-byte content detection
│   ├── fsops/           # Safe filesystem operations and atomic transfers
│   ├── hash/            # Streaming SHA-256 calculation and buffer pool
│   ├── journal/         # Append-only JSONL journal and undo state
│   ├── meta/            # EXIF, MP4/MOV atom, and filename date extraction
│   ├── planner/         # Deterministic execution planning and collision resolution
│   ├── sanitize/        # Cross-OS path and filename sanitization
│   ├── template/        # Path and filename template parsing and rendering
│   └── walk/            # Concurrent directory walker with symlink safety
├── Makefile             # Build, test, and lint targets
├── PROJECT_PLAN.md      # Architectural design and sprint plan
└── LICENSE              # MIT License
```

## Building

Requires Go 1.24+.

```bash
# Build the binary
make build

# Run tests
make test

# Run code vet and tests
make all
```

## License

MIT License. See [LICENSE](LICENSE) for details.
