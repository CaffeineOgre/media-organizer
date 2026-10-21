# ADR-003: CLI Command/Flag Surface and Prompt Flow

- **Status:** Accepted
- **Date:** 2026-10-21
- **Decisions Referenced:** D3, D5, D6, D10, D15, D17, D20, D21, P1, P5, P6, T2.5, T2.6

---

## Context

`media-organizer` must provide an intuitive, reliable, and scriptable command-line interface. Because organizing large media libraries can involve destructive operations (when moving files), mass directory creation, and non-trivial resolution of conflicts, the CLI must:
1. Guarantee safe defaults (copy instead of move, skip duplicates, never guess layouts).
2. Support full two-phase operation (`plan` then `apply`) as well as single-command execution (`organize`).
3. Handle missing or ambiguous configuration predictably: prompt with previews in interactive terminals (TTY), or fail fast with clear actionable error messages in non-interactive/scripted contexts (D10).
4. Strictly enforce prompt timeouts to prevent unattended scripts or headless CI/cron runs from hanging indefinitely (D20).
5. Establish a clear, machine-parseable exit code specification.

---

## Decision

### 1. Command Hierarchy

The CLI binary provides the following command surface:

```text
media-organizer [command] [flags] [arguments]
```

| Command | Usage | Description |
|---|---|---|
| `organize` *(default)* | `media-organizer organize [flags] <source...> <dest>` | Complete workflow: scan source(s), extract metadata, compute plan, present summary/confirmation, and apply operations. |
| `plan` | `media-organizer plan [flags] <source...> <dest>` | Compute execution plan without modifying files. Emits human summary or JSON, optionally writing a `<planfile>` for later execution. |
| `apply` | `media-organizer apply [flags] <planfile>` | Execute a previously computed and verified `<planfile>`. |
| `undo` | `media-organizer undo [flags] <journalfile>` | Reverse operations recorded in a run's journal after verifying destination file integrity (ADR-005). |
| `inspect` | `media-organizer inspect [flags] <file...>` | Read and display raw metadata, magic bytes, extracted dates, datechain resolution, and rendered path for specified files. |
| `config` | `media-organizer config <init\|show>` | Initialize a default TOML configuration file or display currently active configuration with precedence resolution. |

*Note:* If source and destination positional arguments are supplied without a subcommand (e.g. `media-organizer /src /dst`), `organize` is executed by default.

---

### 2. Complete Flag Surface

#### Operational Mode Flags
- `--copy` *(default)*: Copy files from source to destination (D3).
- `--move`: Move files to destination. Moves delete the source file only after post-copy hash verification succeeds (D12).
- `--link`: Create hardlinks from source to destination if on the same filesystem.

#### Layout and Naming Flags
- `--layout <spec>`: Comma-separated layout presets and/or path segment templates (e.g., `type,date-ymd`, `date-ym`). Required if not set in config; prompts in TTY if missing (D10, D13).
- `--rename [template]`: Enable renaming. When provided without arguments, uses default `{YYYY}-{MM}-{DD}-{seq:4}.{ext}`. When provided with an argument, uses that template.
- `--template <template>`: Explicitly set the filename template (implies `--rename`).

#### Collision, Conflict, and Grouping Flags
- `--dup-policy <policy>`: Policy for content-identical files (`skip`, `keep-both`, `duplicates-folder`). Default: `skip` (D5).
- `--seq-conflict <action>`: Resolution strategy when newly processed files have earlier capture times than existing numbered files for that day (`append` or `renumber`). Default: prompt on TTY; error if non-interactive (D15).
- `--sidecars`: Opt-in flag to group sidecar files (`.xmp`, `.aae`, `.thm`, Live Photo pairs, RAW+JPEG) and keep them co-located with primary media (D6).

#### Metadata & Datechain Flags
- `--date-chain <sources>`: Comma-separated order of date sources (`exif,filename,mtime`). Default: `exif,filename,mtime` (D4).
- `--tz <zone>`: IANA timezone identifier (e.g., `America/New_York`, `UTC`, `Asia/Tokyo`). EXIF naive dates are interpreted in this zone; UTC video dates are converted to this zone (D17, D21). Validated against standard Go `time.LoadLocation` at startup.

#### Concurrency and Performance Flags
- `-j, --jobs <N>`: Worker pool concurrency for scanning, magic byte detection, and metadata extraction. Default: `runtime.GOMAXPROCS(0)`.
- `--io-jobs <N>`: Worker pool concurrency for file copying, hashing, and disk writes. Default: `4`.

#### Execution, Automation, and Output Flags
- `--dry-run`: Calculate plan and print summary without performing any filesystem writes.
- `-y, --yes`: Skip confirmation prompt before applying plan.
- `--non-interactive`: Disable all interactive prompts; fail immediately if any user input or decision is required (D10, D15).
- `--prompt-timeout <duration>`: Maximum time to wait for any interactive prompt before aborting (D20). Default: `60s`.
- `--json`: Output report, plan, or inspection in structured JSON format.
- `--config <path>`: Explicit path to TOML configuration file, overriding default platform paths (P6).

---

### 3. Interactive Prompt Flow & TTY Detection

#### Decision Points
Interactive prompts occur under three specific circumstances:
1. **Missing Layout Selection (D10):**
   - Triggered when neither `--layout` nor config defines a layout.
   - Presents a menu with standard presets (`date-ymd`, `date-ym`, `type,date-ymd`, etc.) and a live sample preview rendered from the first 3 scanned media items.
2. **Sequence Conflict Detection (D15):**
   - Triggered when destination directory already contains sequenced files (e.g. `2024-08-15-0001.jpg`), but a newly scanned file has an earlier timestamp than the existing files.
   - Options presented:
     - `[1] Append`: Assign new files the next available sequence numbers at the end (e.g., `0002.jpg`), preserving existing numbers.
     - `[2] Renumber`: Re-sequence all files in true chronological order, staging planned rename operations for existing files.
3. **Plan Confirmation (P1):**
   - Before executing file operations, displays an aggregated plan summary table:
     ```text
     Planned Operations:
       Copies:            1,420 files (14.2 GB)
       Moves:                 0 files
       Duplicates:           12 files (skipped)
       Unverified (mtime):    3 files (routed to Unverified/)
       Sidecars:             45 files (co-located)

     Apply this plan? [y/N]:
     ```

#### TTY & Timeout Enforcement (D10, D20)
- **TTY Detection:** The CLI checks `term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))`.
- **Non-TTY or `--non-interactive`:**
  - If any decision is required (layout missing or sequence conflict detected) and stdin is not a TTY or `--non-interactive` is set, the process terminates immediately with **Exit Code 3**.
  - Stderr receives a concise error explaining the ambiguity and the exact flag needed to resolve it:
    ```text
    error: sequence conflict detected for date 2024-08-15: new file "IMG_0002.jpg" (10:14:02) precedes existing "2024-08-15-0001.jpg" (11:00:15).
    Resolve non-interactively using --seq-conflict=append or --seq-conflict=renumber.
    ```
- **Prompt Timeout (D20):**
  - Every interactive prompt runs with a strict timer (default `60s`, configurable via `--prompt-timeout`).
  - If the timer expires before user input is received:
    ```text
    error: prompt timed out after 60s waiting for user decision. Aborting with no changes made.
    ```
  - The process exits with **Exit Code 3**. No files are created, modified, or deleted.

---

### 4. Configuration Precedence (T1.8)

Configuration settings resolve with the following strict precedence order (highest to lowest):
1. **CLI Flags** (e.g., `--dup-policy=keep-both`)
2. **Environment Variables** (prefixed with `MEDIA_ORGANIZER_`, e.g., `MEDIA_ORGANIZER_DUP_POLICY=keep-both`)
3. **Configuration File** (`config.toml` from `--config` or platform default path P6)
4. **Built-in Defaults** (e.g., copy mode, `skip` duplicates, 60s prompt timeout)

---

### 5. Standard Exit Codes

| Exit Code | Name | Description |
|---|---|---|
| `0` | `ExitSuccess` | All operations completed successfully (or dry-run finished). |
| `1` | `ExitGeneralError` | Unhandled runtime error, I/O failure, permission denied, or disk failure. |
| `2` | `ExitUsageError` | Invalid CLI flags, malformed template syntax, unknown tokens, or invalid timezone. |
| `3` | `ExitDecisionRequired` | Prompt timeout, missing required layout in non-interactive mode, or sequence conflict without resolution flag. |
| `4` | `ExitIntegrityError` | Post-copy hash verification failure, destination file altered during move, or corrupted journal during undo. |

---

## Consequences

### Positive
- Strict non-interactive safety guarantees that unattended automation (scripts, cron jobs) will never hang or make unconfirmed destructive changes.
- Clear, standardized exit codes allow callers to distinguish syntax errors from runtime failures or required decision states.
- Clean separation between planning and execution enables risk-free dry runs and explicit plan inspection.

### Negative / Trade-offs
- Users scripting the tool must explicitly pass flags for layouts and conflict resolutions rather than relying on interactive prompts.
