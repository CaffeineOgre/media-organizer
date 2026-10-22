# ADR-004: Threat Model (STRIDE) and Secure-by-Default Architecture

- **Status:** Accepted
- **Date:** 2026-10-21
- **Task:** T0.5. Bridges Sprint 0 foundations to Sprint 1/2 implementations.
- **Requirements Covered:** S1, S2, S3, S4, S5, S6, S7, S8, S9, S10, S11
- **Decisions Referenced:** D1, D3, D4, D5, D6, D9, D10, D11, D12, D14, D15, D18, D20, D21, P1, P4, P5, P7

---

## 1. Context and Scope

`media-organizer` is a local command-line tool designed to process personal photo and video libraries. Media files are frequently ingested from untrusted or heterogeneous environments: secondhand SD cards, legacy backups, smartphones, downloaded archives, and shared network drives.

Files handled by `media-organizer` may be intentionally corrupted, crafted to exploit parser bugs, structured with hostile directory traversal payloads, or modified mid-operation by concurrent processes. Furthermore, moving files across filesystems carries inherent risk of data loss on power failure or storage exhaustion.

To guarantee user data preservation and host system integrity, `media-organizer` adopts a **secure-by-default, zero-trust architecture**:
1. All input files, metadata tags, filenames, and folder structures are treated as **untrusted user input**.
2. Filesystem state changes must be **non-destructive by default** and **atomic/verifiable when destructive**.
3. Every security requirement (S1–S11) must have an **explicit owning task** and an **automated test**.

---

## 2. System Architecture & Trust Boundaries

```
[ UNTRUSTED SOURCE FILES ] 
  ├── Foreign SD cards, disks, network shares
  ├── Malformed EXIF / IFDs / MP4 atoms
  ├── Hostile filenames (traversal, control chars, reserved names)
  └── Symlink loops and junction redirections
             │
             ▼  [ Trust Boundary 1: Ingestion & Scan ]
  ┌─────────────────────────────────────────────────────────┐
  │ walk (S4, S11)   → Skip symlinks, cycle detection       │
  │ detect (S5)      → Magic bytes verification             │
  │ meta (S5, S11)   → Bounded readers, panic recovery      │
  │ datechain (S11)  → Priority fallback, naive/offset tags │
  │ hash (S11)       → Streaming SHA-256 (pooled memory)    │
  └─────────────────────────────────────────────────────────┘
             │
             ▼  [ Trust Boundary 2: Planning & Path Construction ]
  ┌─────────────────────────────────────────────────────────┐
  │ sanitize (S2, S3) → Traversal filter, NFC, Windows names│
  │ template (S2)     → Startup validation, extension lock  │
  │ planner (S1, S3)  → Deterministic plan, collision policy│
  └─────────────────────────────────────────────────────────┘
             │
             ▼  [ Trust Boundary 3: User Interaction & Execution ]
  ┌─────────────────────────────────────────────────────────┐
  │ cli (S8, S9)     → Terminal sanitization, 60s timeout   │
  │ fsops (S1, S6, S7)→ O_EXCL, temp+fsync, verified delete │
  │ journal (S6, S9) → Append-only JSONL, LIFO verified undo│
  └─────────────────────────────────────────────────────────┘
             │
             ▼  [ Trust Boundary 4: Operating System & Filesystem ]
[ SECURE DESTINATION TREE ]
```

---

## 3. STRIDE Threat Analysis

### 3.1. Spoofing
- **Threat (S-1): File Extension Spoofing.** Malicious executables, scripts, or non-media payloads disguised with media extensions (e.g., `payload.jpg` or `malware.mov`) to trick organization or downstream photo viewers.
  - *Mitigation:* `internal/detect` classifies files using **magic bytes inspection** rather than extensions. Non-media files are categorized as `unknown` and omitted or segregated.
- **Threat (S-2): Timestamp / Metadata Spoofing.** Tampered EXIF headers or manipulated `mtime` values placing sensitive files into unexpected directory locations or colliding with legitimate files.
  - *Mitigation:* Strict datechain precedence (`exif` $\to$ `filename` $\to$ `mtime`, D4). Files dated exclusively via fallback `mtime` are quarantined into the `Unverified/` subtree (D18, P7) to preserve audit visibility.

### 3.2. Tampering
- **Threat (T-1): Path Traversal via Metadata (Zip-Slip / Directory Escape).** EXIF metadata fields like `Make`, `Model`, `Artist`, or software tags containing traversal sequences (e.g. `Model = "../../etc"` or `Artist = "..\..\Windows"`), attempting to write files outside the destination root.
  - *Mitigation:* `internal/sanitize` and `internal/template` enforce strict component-level sanitization: directory separators (`/`, `\`) and traversal tokens (`..`) in metadata tokens are stripped or replaced with safe characters (`-`). Destination paths are validated using `filepath.Rel` to confirm they remain strictly beneath the target directory (S2).
- **Threat (T-2): Symlink Redirection and Poisoning.** Symlinked directories pointing to system paths (e.g. `/etc` or `C:\Windows`) or destination folders pointing through symlinks to overwrite critical system files.
  - *Mitigation:* `internal/walk` does not follow symlinks by default (S4). If `--follow-symlinks` is explicitly enabled, symlink loops are detected using device/inode tracking. Destination file creation explicitly rejects paths that traverse existing symlinks (S4).
- **Threat (T-3): In-Flight File Corruption or Torn Writes.** Power outages, disk disconnections, or filled storage during a move operation leaving partially written files at the destination while deleting the original source.
  - *Mitigation:* All destination writes occur to hidden temporary files in the target directory (`.tmp.<file>.<random>`), followed by `File.Sync()`, pre-move SHA-256 hash comparison against the source file, atomic rename into place, parent directory `fsync`, and only then deletion of the source (D12, S6, P4).

### 3.3. Repudiation
- **Threat (R-1): Unverified or Untraceable File Relocations.** Inability to determine where files originated, what operations succeeded, or how to recover files after an erroneous run.
  - *Mitigation:* Every run records an append-only, durable JSON Lines journal (`.jsonl`) recording source path, destination path, file size, SHA-256 hashes, timestamps, and multi-stage lifecycle states (`planned` $\to$ `copied` $\to$ `verified` $\to$ `source_deleted` $\to$ `done`) (D11, S9).

### 3.4. Information Disclosure
- **Threat (I-1): Supply Chain & Network Leakage.** Malicious or compromised dependencies exfiltrating scanned photo metadata, geolocation tags, or file trees to remote servers.
  - *Mitigation:* Zero network access policy (S7). The binary executes no external processes (`no cgo`, no `exiftool`, no `ffmpeg`, D1). Continuous integration enforces `govulncheck`, `gosec`, and `go mod verify` on all pinned dependencies (S10).
- **Threat (I-2): Terminal Escape Sequence Injection.** Malicious filenames containing ANSI escape codes (e.g., `\x1b[2J`, `\x1b]50;...`) manipulating terminal output, clearing logs, hiding operations, or attempting terminal emulator command execution.
  - *Mitigation:* `internal/cli` and `internal/sanitize` filter control characters (`0x00`–`0x1F`, `0x7F`) and ANSI escape sequences before printing any filename or metadata string to stdout or stderr (S8).

### 3.5. Denial of Service (DoS)
- **Threat (D-1): Hostile Parser Payloads (Parser Bombs).** Crafted media files containing recursive EXIF IFD pointer cycles, 64-bit integer atom sizes claiming 16 EiB, or 50,000-deep nested `moov`/`trak` boxes causing infinite loops, stack overflow, or memory exhaustion.
  - *Mitigation:* All media parsers enforce hard resource bounds: maximum bytes read per file via `io.LimitReader`, maximum IFD recursion depth, maximum atom nesting depth, bounded per-file processing timeouts, and panic recovery enclosing each file parsing routine (S5).
- **Threat (D-2): Memory Exhaustion via Whole-File Buffering.** Attempting to read large multi-gigabyte video or RAW files entirely into RAM for hashing or metadata extraction.
  - *Mitigation:* Streaming I/O only. Hashes are computed using fixed-size buffer pools (`sync.Pool` with 64 KiB chunks); metadata extraction reads only container headers (S11).
- **Threat (D-3): Unattended Script Hanging.** Headless processes hanging indefinitely awaiting interactive terminal input when an unexpected conflict or missing option occurs.
  - *Mitigation:* Interactive prompts enforce a strict 60-second timeout (`--prompt-timeout`, D20). Headless or non-interactive runs (`--non-interactive` or `!isatty`) immediately abort with Exit Code 3 and zero modifications (D10, D15).

### 3.6. Elevation of Privilege
- **Threat (E-1): Excessive File Permissions and Executable Flags.** Organized media files gaining execute permissions (`+x`) or world-writable permissions during copying or directory creation.
  - *Mitigation:* Preserved permissions: destination files never receive wider permissions than their source file. Created directories are capped at `0o755` masked by system umask (S7). No setuid, elevated privileges, or subprocess execution permitted (D1, S7).

---

## 4. Secure-by-Default Architectural Invariants

The following invariants are hardcoded into the architecture:

| Invariant | Default Setting | Override Mechanism | Security Rationale |
|---|---|---|---|
| **Operation Type** | `Copy` | `--move` | Prevents accidental deletion of source media (D3). |
| **File Overwrite** | `O_EXCL` (Never overwrite) | None | Existing files are never clobbered; collisions use collision policy (S1). |
| **Symlink Traversal** | Disabled | `--follow-symlinks` | Prevents symlink traversal out of source trees or cycle loops (S4). |
| **Destination Symlinks** | Disallowed | None | Destination paths cannot write through symlinks (S4). |
| **Collision Resolution** | `Skip & Report` | `--dup-policy` | Content-identical files are never replaced or deleted silently (D5). |
| **Mtime Quarantining** | Routed to `Unverified/` | `unverified_dir` config | Separates unverified timestamp inferences from verified metadata (D18). |
| **Interactive Prompts** | 60s timeout | `--prompt-timeout` | Prevents headless execution from hanging indefinitely (D20). |
| **Ambiguity Handling** | Fail fast with error | None | CLI never silently guesses layouts or conflict policies (D10, D15). |
| **Destructive Move** | Post-copy verified delete | None | Source is deleted only after destination fsync & hash match (D12, S6). |
| **Subprocess Execution** | Completely forbidden | None | Zero exec, zero shell, pure Go only (D1, S7). |
| **Network Egress** | Zero sockets | None | Air-gapped design; local files only (S7). |

---

## 5. Traceability Matrix: Mapping S1–S11 to Owning Tasks and Tests

Every security requirement has an identified owning implementation task, a responsible Go package, and an automated verification test suite:

| ID | Requirement & Invariant | Implementation Task | Responsible Package | Verification Test Strategy | Owning Test Suite |
|---|---|---|---|---|---|
| **S1** | **No overwrite, ever.** `O_EXCL` creation; collisions handled by policy, never clobber. | **T1.9**, T2.1 | `internal/fsops`<br/>`internal/planner` | Attempt to write to existing destination paths under copy, move, and collision scenarios; assert `EEXIST` error or policy branching, zero file overwriting. | `internal/fsops/fsops_test.go`<br/>`internal/planner/planner_test.go` |
| **S2** | **No path traversal.** Sanitized components; verify path resolves strictly under destination root. | **T1.7**, T1.6 | `internal/sanitize`<br/>`internal/template` | Path traversal fuzzing with `../`, `..\\`, absolute paths, volume tags (`C:`), and embedded metadata payloads (`Model="../../etc/passwd"`). | `internal/sanitize/sanitize_test.go`<br/>`internal/template/template_test.go` |
| **S3** | **Cross-OS safe names.** Strip control chars, illegal characters, Windows reserved names; NFC normalize; cap 255 bytes; case-fold collision check. | **T1.7**, T2.1 | `internal/sanitize`<br/>`internal/planner` | Cross-platform table-driven tests for Windows reserved names (`CON.jpg`, `LPT1`), control characters (`0x00`-`0x1F`), NFC vs NFD unicode equivalence, and case-insensitive collisions on macOS/Windows. | `internal/sanitize/sanitize_test.go`<br/>`internal/planner/collision_test.go` |
| **S4** | **Symlinks not followed.** Directories not descended; cycle detection with opt-in; no writing through destination symlinks. | **T1.1**, T1.9 | `internal/walk`<br/>`internal/fsops` | Directory walker tests on symlink loops, external directory symlinks, Windows junction points; verify symlinks skipped by default; verify destination writes refuse symlinked targets. | `internal/walk/walk_test.go`<br/>`internal/fsops/symlink_test.go` |
| **S5** | **Hostile file parsing.** Bounded readers; depth caps on IFDs and atoms; per-file panic recovery and timeout. | **T1.3**, T1.4, T1.5 | `internal/meta`<br/>`internal/detect` | Fuzz testing (`go test -fuzz`) and hostile fixture corpus test: oversized 4 GiB atoms, 5,000 nested atoms, circular IFD pointers, truncated files; assert no panics, no hangs, clean error return. | `spike/metadata/fuzz_test.go`<br/>`internal/meta/meta_test.go`<br/>`T3.2` Fuzz Campaign |
| **S6** | **Integrity.** Post-write hash verification; source deleted only after verified copy; fsync file and dir; undo checks hashes before reversing. | **T1.9**, T2.4 | `internal/fsops`<br/>`internal/journal` | Fault-injection tests: simulate write failures, hash mismatches during cross-device move, modified destination file during undo; verify source file preserved and modified files never deleted. | `internal/fsops/fsops_test.go`<br/>`internal/journal/undo_test.go` |
| **S7** | **Least privilege.** No network access, no subprocess execution, permission preservation, dirs created `0o755`. | **T1.9**, T0.1 | `internal/fsops`<br/>`cmd/media-organizer` | Inspect binary with `gosec` for exec/network imports; verify copied/moved files preserve original `FileMode` (capped without SUID/SGID/execute); verify directories created with safe permissions. | `internal/fsops/perms_test.go`<br/>`Makefile` (`make sec`) |
| **S8** | **Safe output.** Sanitize filenames before printing to prevent terminal escape injection. | **T1.7**, T2.5 | `internal/sanitize`<br/>`internal/cli` | Output tests passing filenames containing ANSI escape sequences (`\x1b[31m`, `\x1b[2J`) through logger/formatter; assert escape bytes replaced with `?` or stripped. | `internal/cli/format_test.go`<br/>`internal/sanitize/escape_test.go` |
| **S9** | **Interruption safety.** SIGINT/SIGTERM drain in-flight ops, cleanup temp files, flush journal; `--resume` recovers cleanly. | **T2.4**, T2.5 | `internal/journal`<br/>`internal/cli` | Process interruption tests (sending `SIGINT` during active copy); verify journal flushed, zero orphaned `.tmp.*` files remaining in destination, `--resume` completes remaining operations. | `internal/journal/resume_test.go`<br/>`cmd/media-organizer/signal_test.go` |
| **S10** | **Supply chain integrity.** Minimal pinned deps, `govulncheck`, `gosec`, `go mod verify`, reproducible builds (`-trimpath`), SBOM. | **T0.2**, T3.6 | `CI/CD`<br/>`cmd/media-organizer` | GitHub Actions CI matrix running `go mod verify`, `go tool gosec`, `go tool govulncheck`; build validation comparing `-trimpath` reproducible checksums; Goreleaser SBOM generation. | `.github/workflows/ci.yml`<br/>`Makefile` (`make vulncheck sec`) |
| **S11** | **Resource limits.** Bounded worker pools, buffer pool reuse, no full-file RAM buffering, file descriptor caps. | **T1.1**, T1.2, T1.9 | `internal/walk`<br/>`internal/hash`<br/>`internal/fsops` | Benchmarks and memory profiling on 10 GB+ multi-file sets; verify memory consumption remains sub-50 MB; verify active file descriptors do not exceed worker pool caps (`-j` + `--io-jobs`). | `internal/hash/hash_test.go`<br/>`T3.4` Benchmarks |

---

## 6. Consequences & Implementation Constraints

1. **For `internal/walk` (T1.1):**
   - Must use `os.Lstat` instead of `os.Stat` to detect symlinks before descending.
   - Must track directory device/inode pairs to prevent recursive symlink loops when `--follow-symlinks` is active.
2. **For `internal/meta` (T1.3, T1.4):**
   - All parser invocations must be wrapped with `io.LimitReader` and a `defer func() { recover() }()` guard to prevent hostile payloads from crashing the binary.
3. **For `internal/sanitize` (T1.7):**
   - Must be implemented as a pure, dependency-free package handling component and full-path normalization, UTF-8 validity, NFC conversion, and Windows device name escaping.
4. **For `internal/fsops` (T1.9):**
   - Cross-device moves must follow the strict 10-step verified move protocol defined in ADR-005. No shortcuts or unverified deletes are permitted.
5. **For `internal/journal` (T2.4):**
   - Every state transition must be atomic and synchronous (`fsync`). The undo engine must refuse to touch any file whose hash has changed since organization.
