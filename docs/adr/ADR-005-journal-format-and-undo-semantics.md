# ADR-005: Journal Format and Undo Semantics

- **Status:** Accepted
- **Date:** 2026-10-21
- **Decisions Referenced:** D11, D12, S6, S9, T2.4

---

## Context

When organizing thousands of files, errors can happen: power outages, full disks, accidental user choices, or unintended moves. To maintain absolute data integrity and user trust, `media-organizer` requires:
1. **Crash-Resilient State Tracking:** Every plan execution must record all operations in an append-only, durable format that allows resuming interrupted runs (S9).
2. **Guaranteed Move Safety:** When moving files across devices, source files must **only** be deleted after destination writes are fully flushed to disk and cryptographically verified by hash (D12, S6).
3. **Safe, Verifiable Undo:** Any completed or partially completed run must be fully reversible via `media-organizer undo <journal>`, with cryptographic checks ensuring no altered files are inadvertently deleted (D11).

---

## Decision

### 1. Journal File Format

The journal is stored as **JSON Lines (JSONL)**: append-only, human-readable, stream-parseable, and durable against unexpected process termination.

#### File Naming and Location
By default, the journal file is stored at the root of the destination tree:
```text
<destination>/.media-organizer-journal-<YYYYMMDD-hhmmss>-<run_id>.jsonl
```
Users can specify an alternative journal destination using `--journal <path>`.

#### Record 1: Header Line
The first line of every journal file contains a metadata header object:

```json
{
  "type": "header",
  "schema_version": 1,
  "run_id": "f81d4fae-7dec-11d0-a765-00a0c91e6bf6",
  "started_at": "2026-10-21T14:30:00Z",
  "tool_version": "v0.1.0",
  "command": "organize",
  "flags": {
    "move": true,
    "layout": "type,date-ymd",
    "rename": true,
    "dup_policy": "skip",
    "tz": "America/New_York"
  },
  "source_roots": ["/Volumes/SDCard/DCIM"],
  "dest_root": "/Volumes/Storage/Media"
}
```

#### Subsequent Records: Operation State Lines
Every filesystem operation is recorded as a line in the journal. Multi-step operations (like cross-device moves) emit state updates sharing the same `op_id`:

```json
{
  "type": "op",
  "op_id": 104,
  "kind": "move_cross_device",
  "state": "planned",
  "timestamp": "2026-10-21T14:30:05.120Z",
  "src_path": "/Volumes/SDCard/DCIM/100CANON/IMG_0042.CR3",
  "dst_path": "/Volumes/Storage/Media/Raw/2024/08/15/2024-08-15-0042.cr3",
  "file_size": 34125824,
  "src_hash": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
  "dst_hash": "",
  "mtime": "2024-08-15T14:02:18Z",
  "is_sidecar": false,
  "group_id": "group-0042"
}
```

#### Operation Kinds
- `mkdir`: Directory creation.
- `copy`: File copy.
- `move_same_device`: Fast-path atomic rename on the same filesystem.
- `move_cross_device`: Copy -> verify hash -> atomic rename into place -> delete source.
- `hardlink`: Hardlink creation.

#### Operation States
- `planned`: Operation queued.
- `copied`: Bytes written to temporary file and `fsync`'d.
- `verified`: Checksum of written copy matches source checksum.
- `source_deleted`: Source file successfully deleted (for moves).
- `done`: Operation fully concluded.
- `failed`: Operation failed; includes `"error": "<description>"`.

#### Durability & Fsync Policy
- After writing critical state lines (e.g., `verified`, `source_deleted`, `done`), the journal file handle is flushed to disk via `file.Sync()`.

---

### 2. Move Verification Semantics (D12)

#### Same-Device Move (Fast Path)
1. Check if source and destination reside on the same filesystem device (`stat.Sys().(*syscall.Stat_t).Dev`).
2. If same device: invoke atomic filesystem rename `os.Rename(src, dst)`.
3. If successful, record `state: "done"`. No data copy or verification is required.

#### Cross-Device Move (Verified Safe Path)
If source and destination reside on different devices/mounts:
1. **Hash Source:** Read source file, calculating streaming SHA-256 (`src_hash`).
2. **Write Temp File:** Create a hidden temporary file in the *destination directory*:
   `<dst_dir>/.tmp.<filename>.<random>` with flags `O_CREATE | O_WRONLY | O_EXCL` and mode matching source permissions (or `0o644`).
3. **Stream Copy & Hash:** Copy bytes from source to temp file, simultaneously computing streaming SHA-256 (`dst_hash`).
4. **Fsync Temp File:** Call `tempFile.Sync()` to ensure all bytes are committed to physical storage before verifying.
5. **Checksum Verification:**
   - Compare `src_hash` and `dst_hash`.
   - **If hashes do not match:** Immediately close and delete the temporary file (`os.Remove(temp)`). **The source file is left untouched.** Emit journal record `state: "failed", error: "checksum mismatch"`.
6. **Atomic Rename into Place:** `os.Rename(tempPath, dstPath)`.
7. **Fsync Destination Directory:** Call `dir.Sync()` on destination directory to ensure directory entry is persisted.
8. **Record Verified State:** Append journal record `state: "verified"`.
9. **Delete Source:** Delete original source file: `os.Remove(src)`.
10. **Record Done State:** Append journal record `state: "done"`.

*Guarantee:* Under no condition is the source file deleted unless the destination file exists, is fsync'd, and its hash matches the source file bit-for-bit.

---

### 3. Undo Semantics (D11)

The undo command reverses the actions of a previous run:
```bash
media-organizer undo <journal.jsonl> [--dry-run] [--yes]
```

#### Reversal Order
The journal file is read completely, and finished operations are processed in **strict reverse chronological order** (LIFO: last operation first).

#### Pre-Condition Hash Verification
Before removing or moving any file during an undo, the file must be verified:

1. **Undoing a `copy` (or `hardlink`):**
   - Check if destination file exists at `dst_path`. If missing: log warning, skip.
   - Read and compute SHA-256 checksum of `dst_path`.
   - Compare current checksum against `dst_hash` recorded in the journal.
   - **If checksum matches:** Delete destination file: `os.Remove(dst_path)`.
   - **If checksum DOES NOT match (file has been modified since run):**
     - **DO NOT DELETE.**
     - Log warning: `warning: destination file "<dst>" was modified since organization; skipping deletion to prevent data loss`.
     - Skip file and continue.

2. **Undoing a `move` (same-device or cross-device):**
   - Check if destination file exists at `dst_path`. If missing: log error, skip.
   - Read and compute SHA-256 checksum of `dst_path`.
   - Verify checksum equals `src_hash`. If altered, skip with warning.
   - Check if `src_path` is currently occupied:
     - **If `src_path` already exists:** **REFUSE TO OVERWRITE.** Log warning: `warning: original source path "<src>" is currently occupied; skipping move back`.
   - If `src_path` is clear and checksum matches:
     - Move file from `dst_path` back to `src_path` (using atomic rename or cross-device verified move).

3. **Undoing `mkdir`:**
   - Attempt to remove directory `os.Remove(dir)`.
   - If directory contains files (e.g. unreversed modified files or files added later), `os.Remove` fails harmlessly (`directory not empty`). Empty directories created by the run are cleanly removed.

#### Undo Journaling and Crash Safety (S9)
- Undo operations write their own execution journal:
  `<journal>.undo-<timestamp>-<undo_run_id>.jsonl`
- If an undo operation is interrupted (e.g. SIGINT/Ctrl+C), running `media-organizer undo` again resumes safely by reconciling the original journal against the undo journal.

#### Summary Report
When undo finishes (or in `--dry-run`), a summary report is printed:
```text
Undo Summary:
  Copies Removed:       1,420 files
  Moves Restored:           0 files
  Directories Removed:     38 dirs
  Skipped (Modified):       1 files (preserved)
  Skipped (Missing):        0 files
```

---

## Consequences

### Positive
- Move operations are completely resilient against data loss: corrupt copies or power failures never destroy source data.
- Undo operations are safe by design: modified or missing destination files will never be blindly deleted.
- Line-delimited append-only journal survives process crashes, system halts, and enables safe resumption.

### Negative / Trade-offs
- Cross-device moves require double the storage bandwidth (reading source, writing dest, hashing both, then deleting source) compared to unverified moves. This is an explicit, necessary design trade-off for data safety.
