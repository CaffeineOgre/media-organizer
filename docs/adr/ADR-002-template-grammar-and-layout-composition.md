# ADR-002: Template Grammar, Token List, and Layout Composition

- **Status:** Accepted
- **Date:** 2026-10-21
- **Decisions Referenced:** D7, D8, D13, D14, D18, S2, S3, P7

---

## Context

`media-organizer` organizes media files (photos, videos, RAW files, sidecars) into structured destination directories and optionally renames them. To support diverse user preferences, the folder structure and naming scheme must be configurable, extensible, deterministic, and safe across Linux, macOS, and Windows.

Key requirements:
1. Support combining predefined organizational schemes (e.g., by date, by media type) in any user-defined order (D13).
2. Allow custom folder and filename templates with metadata tokens (D7, D8).
3. Preserve original filenames by default when no renaming is requested (D14).
4. Clearly segregate files whose date could only be inferred from filesystem modification time (`mtime`) into an `Unverified/` tree (D18).
5. Prevent path traversal attacks or invalid cross-OS filenames from metadata values (S2, S3).
6. Validate all templates at startup to fail fast before processing any files.

---

## Decision

### 1. Template Syntax and Grammar

Template strings use a curly-brace token syntax: `{token}` or `{token:argument}`.

- **Token Delimiters:** `{` begins a token; `}` ends a token.
- **Literal Escaping:**
  - `{{` renders as a literal `{`.
  - `}}` renders as a literal `}`.
- **Path Separators:**
  - Forward slash `/` is the universal logical path separator within templates across all operating systems.
  - Windows backslashes `\` in CLI flags or configuration are normalized to `/` during template compilation.
  - Empty path segments (e.g., `a//b`, leading `/`, trailing `/`) are normalized away.
- **Whitespace & Literals:** Literal text outside of `{...}` tokens (e.g., `-`, `_`, spaces, alphanumeric characters) is preserved verbatim, subject to path sanitization.

#### Formal EBNF Grammar

```ebnf
Template        = { Segment } ;
Segment         = LiteralText | EscapedBrace | Token ;
EscapedBrace    = "{{" | "}}" ;
Token           = "{" TokenName [ ":" TokenArg ] "}" ;
TokenName       = "YYYY" | "YY" | "MM" | "MMM" | "DD"
                | "hh" | "mm" | "ss"
                | "seq" | "type" | "ext" | "origext"
                | "orig" | "make" | "model" | "source" ;
TokenArg        = Digit { Digit } ;
LiteralText     = ? any unicode character except '{' and '}' ? ;
Digit           = "0" | "1" | "2" | "3" | "4" | "5" | "6" | "7" | "8" | "9" ;
```

---

### 2. Complete Token Specification

| Token | Description | Example Output | Constraints / Notes |
|---|---|---|---|
| `{YYYY}` | 4-digit capture year | `2024` | Zero-padded (0001–9999) |
| `{YY}` | 2-digit capture year | `24` | Last 2 digits (00–99) |
| `{MM}` | 2-digit capture month | `08` | Zero-padded (`01`–`12`) |
| `{MMM}` | 3-letter English month name | `Aug` | Title-case ASCII: `Jan`, `Feb`, `Mar`, `Apr`, `May`, `Jun`, `Jul`, `Aug`, `Sep`, `Oct`, `Nov`, `Dec` |
| `{DD}` | 2-digit capture day of month | `15` | Zero-padded (`01`–`31`) |
| `{hh}` | 2-digit hour (24-hour clock) | `14` | Zero-padded (`00`–`23`) |
| `{mm}` | 2-digit minute | `05` | Zero-padded (`00`–`59`) |
| `{ss}` | 2-digit second | `30` | Zero-padded (`00`–`59`) |
| `{seq:N}` | Daily sequence number | `0001` (for `N=4`) | `N` is width (1 ≤ `N` ≤ 9). Default `N=4` if omitted (`{seq}`). Assigned deterministically per day by capture time. |
| `{type}` | Media classification | `Photos`, `Videos`, `Raw` | Derived from MIME / magic bytes classification. |
| `{ext}` | Lowercased file extension | `jpg`, `mov`, `cr3` | Without leading dot. |
| `{origext}`| Original-case file extension | `JPG`, `MOV`, `dng` | Without leading dot. |
| `{orig}` | Original filename stem | `IMG_4021` | Base filename without extension; sanitized. |
| `{make}` | Camera / device manufacturer | `Apple`, `Canon` | Trimmed, sanitized. Empty string if unavailable. |
| `{model}` | Camera / device model | `iPhone 15 Pro`, `EOS R5` | Trimmed, sanitized. Empty string if unavailable. |
| `{source}` | Resolved date source | `exif`, `filename`, `mtime` | Indicates datechain origin. |

#### Token Fallback & Missing Value Semantics
- If `{make}` or `{model}` metadata is not present in the media item, it evaluates to an empty string `""`.
- Any resulting adjacent duplicate separators (e.g. `{make}_{model}` -> `_EOS R5` or `Canon_` or `""`) have leading/trailing delimiters trimmed according to standard component sanitization.

---

### 3. Layout Schemes and Composition (D13, D18)

#### Layout Presets
To avoid requiring users to type verbose templates for common folder structures, the tool defines the following built-in layout presets:

| Preset Name | Expands To | Sample Render |
|---|---|---|
| `date-ymd` | `{YYYY}/{MM}/{DD}` | `2024/08/15` |
| `date-ym` | `{YYYY}/{MM}` | `2024/08` |
| `date-y` | `{YYYY}` | `2024` |
| `type` | `{type}` | `Photos` |

#### Layout Composition
The `--layout` flag accepts a comma-separated list of preset names and/or custom path segment templates. Segments are joined in the specified order with `/`:

- `--layout type,date-ym`
  - Pattern: `{type}/{YYYY}/{MM}`
  - Example: `Photos/2024/08`
- `--layout date-ym,type`
  - Pattern: `{YYYY}/{MM}/{type}`
  - Example: `2024/08/Photos`
- `--layout date-y,type,date-ymd`
  - Pattern: `{YYYY}/{type}/{YYYY}/{MM}/{DD}`
  - Example: `2024/Photos/2024/08/15`
- Custom segments combined with presets:
  - `--layout type,{YYYY}/{YYYY}-{MM}`
  - Example: `Photos/2024/2024-08`

#### Mtime Fallback: The `Unverified/` Subtree (D18, P7)
When a file's capture date cannot be obtained from embedded metadata or filename patterns, the datechain falls back to filesystem modification time (`mtime`).
- Any media item resolved via `mtime` has an `Unverified/` directory prepended to its relative destination path:
  - Normal item: `Photos/2024/08/IMG_0001.jpg`
  - Mtime item: `Unverified/Photos/2024/08/IMG_0001.jpg`
- The prefix name defaults to `Unverified` and is configurable via `unverified_dir` in configuration (P7).

---

### 4. Filename Renaming Rules (D14)

#### Default Behavior (No `--rename`)
When `--rename` is **not** supplied:
- Files **keep their original filename** (D14).
- The template effectively defaults to: `{orig}.{origext}`.
- Original extension casing is preserved (e.g., `IMG_0042.JPG` remains `IMG_0042.JPG`).
- Collision policies (e.g. appending collision suffix `-1`) still apply if another file occupies the destination path.

#### Renaming with `--rename`
When `--rename` is supplied:
1. **Flag without argument (`--rename`)**:
   - Uses the default rename template: `{YYYY}-{MM}-{DD}-{seq:4}.{ext}`
   - Extension is normalized to lower case.
   - Example: `2024-08-15-0001.jpg`
2. **Flag with custom template (`--rename="{YYYY}{MM}{DD}_{orig}.{ext}"`)**:
   - Renders the specified custom template.

---

### 5. Template Validation Rules (Startup Fast-Fail)

The template compiler validates all templates before any scanning or filesystem operations commence:

1. **Syntax Validation:**
   - Stray `{` or `}` characters without matching pairs or escapes trigger a configuration error.
   - Unrecognized token names (e.g., `{year}`) trigger an immediate error listing valid tokens.
   - Invalid arguments (e.g., `{seq:0}`, `{seq:12}`, `{seq:abc}`) trigger a syntax error.
2. **Filename Extension Requirement:**
   - Any template designated as a filename template **must** contain `{ext}` or `{origext}`.
   - Templates omitting the extension token are rejected at startup to prevent mass stripping of file extensions.
3. **Sequence Token Placement:**
   - `{seq}` or `{seq:N}` is only valid in filename templates, not directory layout segments.
4. **Security & Path Traversal Checks (S2, S3):**
   - Filename templates cannot contain `/` or `\` (path separators belong exclusively in layout definitions).
   - Dynamic token replacements (`{orig}`, `{make}`, `{model}`) cannot contain `..`, leading/trailing slashes, or volume specifiers.

---

### 6. Sanitization Pipeline Integration (S2, S3)

Each rendered path segment (directory name or filename) passes through `sanitize.Component()`:
1. Control characters (`0x00`–`0x1F`, `0x7F`) are removed.
2. Cross-OS illegal characters (`/ \ : * ? " < > |`) are replaced with `-` or removed.
3. Trailing dots and spaces (illegal on Windows) are trimmed.
4. Windows reserved names (`CON`, `PRN`, `AUX`, `NUL`, `COM1`–`COM9`, `LPT1`–`LPT9`, with or without extensions) receive an underscore prefix (e.g., `_CON.jpg`).
5. Unicode strings are normalized to **NFC** format.
6. Byte length is capped at 255 bytes (UTF-8 safe truncation preserving file extensions).

---

## Consequences

### Positive
- Flexible and composable layout options without requiring shell scripting or multiple passes.
- Safe defaults: files retain original names unless explicit renaming is opted in.
- Strict startup validation ensures typos in CLI flags or config files fail immediately before altering any files.
- Clear audit trail for unverified dates via the `Unverified/` subtree.

### Negative / Trade-offs
- Users accustomed to other tools' arbitrary date format specifiers (e.g. `%Y/%m/%d`) must learn the `{YYYY}/{MM}/{DD}` token syntax.
- Startup extension validation prevents generating extensionless destination files (an intentional safety feature).
