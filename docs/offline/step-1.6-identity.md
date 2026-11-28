# Step 1.6 — Identity (load/save/delete)

## 1. Goal and why it exists in the system (3-5 sentences)

The agent needs to remember who it is across restarts so it does not re-register
every time. Step 1.6 stores a worker's ID and secret in a small JSON file
(`identity.json`) in a data directory. On startup the agent loads it; if the file
is missing (first run or forgotten identity) the agent registers and saves a
new one. Deleting the file is how the agent "forgets" a revoked identity. The
file must be written atomically and with restrictive permissions so the
secret never leaks via a partial write or world-readable file.

## 2. Go concepts I need first

**`os.ReadFile` / `os.WriteFile` and `os.Stat`.**
Read/write files. `os.Stat` returns FileInfo with mode; `os.IsNotExist(err)`
checks missing file.

```go
data, err := os.ReadFile("f.txt")
if os.IsNotExist(err) { /* missing */ }
```

**`json.Marshal` / `json.Unmarshal`.**
Convert structs to/from JSON bytes. Tags control field names.

```go
type T struct { Name string `json:"name"` }
b, _ := json.Marshal(T{Name: "x"})
var t T; json.Unmarshal(b, &t)
```

**`os.MkdirAll`.**
Creates directory (and parents) if missing; no-op if exists; sets perms on
creation only.

```go
os.MkdirAll("a/b/c", 0700)
```

**`os.CreateTemp` + `os.Rename` (atomic write).**
Write to temp file in same directory, then rename into place. Rename is
atomic on same filesystem. Final file gets temp's mode (set via Chmod).

```go
f, _ := os.CreateTemp(dir, "tmp-*")
f.Write(data); f.Close()
os.Chmod(f.Name(), 0600)
os.Rename(f.Name(), finalPath)
```

**`errors.Is` and sentinel errors.**
Sentinel errors are package-level `var`s; callers use `errors.Is` to check
without string matching.

```go
var ErrNotFound = errors.New("not found")
if errors.Is(err, ErrNotFound) { /* handle */ }
```

**Pointers and nil.**
`*Identity` return allows nil on error; callers nil-check.

```go
func load() (*T, error) { return nil, ErrX }
```

## 3. Exact spec: types, signatures, rules

```go
type Identity struct {
    WorkerID string `json:"worker_id"`
    Secret   string `json:"secret"`
}
var ErrNoIdentity = errors.New("no saved identity")
func LoadIdentity(dir string) (*Identity, error)   // missing file -> ErrNoIdentity
func SaveIdentity(dir string, id Identity) error
func DeleteIdentity(dir string) error              // missing file is NOT an error
```

Rules:
- File name: `identity.json` inside `dir`.
- `MkdirAll(dir, 0700)` before writing.
- Write to a temp file **in the same dir**, then `os.Rename`. Final mode `0600`.
- JSON tags `worker_id`, `secret`.
- `LoadIdentity`: missing file → `ErrNoIdentity`; corrupt JSON → a different error (not `ErrNoIdentity`).
- `DeleteIdentity`: removes the file; safe to call twice (missing file → nil).

## 4. Algorithm in plain-English numbered steps (no Go code)

**LoadIdentity(dir):**
1. Build path `dir/identity.json`.
2. `os.ReadFile`. If file missing → return `nil, ErrNoIdentity`.
3. If other read error → return `nil, err`.
4. `json.Unmarshal` bytes into `Identity`. If parse error → return `nil, err` (not `ErrNoIdentity`).
5. Return `&id, nil`.

**SaveIdentity(dir, id):**
1. `os.MkdirAll(dir, 0700)`.
2. Marshal `id` to JSON.
3. Create temp file in `dir` (same filesystem for atomic rename).
4. Write JSON to temp file.
5. `Chmod` temp file to `0600`.
6. Close temp file.
7. `os.Rename(temp, dir/identity.json)`.
8. On any error, remove temp file; return err.
9. Return nil.

**DeleteIdentity(dir):**
1. `os.Remove(dir/identity.json)`.
2. If missing file (`os.IsNotExist`) → return nil (not an error).
3. Other error → return err.
4. Return nil.

## 5. List of tests

| Test name | What it asserts | Why it matters |
|---|---|---|
| `TestIdentitySaveLoadRoundTrip` | save then load returns equal data | core round-trip |
| `TestIdentityFileMode` | `os.Stat(...).Mode().Perm() == 0600` | secret not world-readable |
| `TestIdentityDirMode` | directory perm `0700` (use a non-existent subdir) | directory not world-accessible |
| `TestLoadIdentityMissingReturnsErrNoIdentity` | load from empty dir → `errors.Is(err, ErrNoIdentity)` | first-run detection |
| `TestLoadIdentityCorruptJSON` | corrupt JSON → error that is NOT `ErrNoIdentity` | distinguish missing vs broken |
| `TestIdentitySaveTwiceOverwrites` | saving twice leaves latest data | re-registration overwrites |
| `TestIdentityNoTmpFilesLeft` | no `.tmp` files after save | atomic write cleans up |
| `TestDeleteIdentity` | removes file; safe to call twice | forget identity works |

## 6. Commands to run and exact output when done

```bash
cd ~/Desktop/node-agent
go vet ./internal/agent
go test -race ./internal/agent -run TestIdentity -v
go test -race ./...
```

Expected: all `TestIdentity*` PASS, all packages ok, no FAIL. (Tests fail until implemented.)

## 7. Common mistakes and how tests reveal them

| Mistake | Symptom | Test |
|---|---|---|
| Write directly (not temp+rename) | Partial file on crash; no atomicity | `TestIdentityNoTmpFilesLeft` (less direct); correctness |
| Wrong file mode (0644) | Secret readable by others | `TestIdentityFileMode` |
| `MkdirAll` on existing dir changes nothing | Dir stays 0755 | `TestIdentityDirMode` (use non-existent subdir) |
| Missing file returns generic error | `errors.Is` fails | `TestLoadIdentityMissingReturnsErrNoIdentity` |
| Corrupt JSON returns `ErrNoIdentity` | Conflates missing with broken | `TestLoadIdentityCorruptJSON` |
| Temp file left behind | `.tmp` files accumulate | `TestIdentityNoTmpFilesLeft` |
| `DeleteIdentity` panics on missing | Crash on second call | `TestDeleteIdentity` |

## 8. 5 self-check questions (answers at the very end)

1. Why write to a temp file and rename instead of writing directly?
2. Why `MkdirAll(dir, 0700)` and why must the dir not exist before the test?
3. Why `0600` for the file and `0700` for the dir?
4. Why distinguish missing file (`ErrNoIdentity`) from corrupt JSON?
5. Why is `DeleteIdentity` safe to call twice?

**Answers:**
1. `os.Rename` is atomic on the same filesystem; a crash mid-write leaves the old file intact. Writing directly can leave a truncated/partial file.
2. `MkdirAll` creates with 0700 only if the dir doesn't exist; on an existing dir it's a no-op and keeps old perms. Using a non-existent subdir tests creation.
3. `0600` = owner read/write only; `0700` = owner rwx only. The secret must not be readable by other users.
4. Missing file means first run (register); corrupt JSON means a bug or tampering (different recovery). Conflating them causes wrong behavior.
5. `os.Remove` on a missing file returns `os.IsNotExist`; we treat that as success. Idempotent delete is safer for "forget identity" flows.

## 9. Done checklist

- [ ] `internal/agent/identity.go` implements all three functions
- [ ] `go vet ./internal/agent` clean
- [ ] `go test -race ./internal/agent -run TestIdentity -v` all PASS
- [ ] `go test -race ./...` all packages PASS
- [ ] File mode 0600, dir mode 0700
- [ ] Atomic write (temp + rename), no leftover .tmp
- [ ] Missing file → ErrNoIdentity; corrupt → different error
- [ ] DeleteIdentity idempotent