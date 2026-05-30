# Snapshot Format

Every hermes-operator snapshot is a raw `.tar.zst` archive of `/home/hermes/.hermes/` with a `meta.json` entry. Each snapshot is stored as one S3 object. The bucket has no repository layout, snapshot tags, or pack files. The format is stable across v1.x.

## Layout (inside the tar)

```
./                        # everything under /home/hermes/.hermes
./skills/
./profiles/
./config.yaml
./db/...                  # FTS5 session memory
meta.json                 # archive metadata entry
```

## `meta.json`

```json
{
  "instance_uid": "9d3d8a7b-91a7-4c2e-8e3a-7c2e8b1d8a91",
  "timestamp": "2026-05-10T03-00-00Z",
  "format_version": 1
}
```

| Field | Meaning |
|---|---|
| `instance_uid` | The HermesInstance's `metadata.uid` at backup time. Used by the operator to detect cross-instance restores. |
| `timestamp` | UTC timestamp with `:` and `.` replaced by `-` for filesystem safety. |
| `format_version` | Currently `1`. Bumped when the layout changes incompatibly. |

## Compression

`zstd -T0 -19` (long-range, max compression, all cores). Typical compression ratio on hermes data is 5-8x (FTS5 indexes compress especially well).

## Encryption

Encryption is **not** built into the snapshot format. Use bucket-side encryption such as SSE-S3 or SSE-KMS. The operator's backup and restore jobs expect raw `.tar.zst` objects and do not read or write encrypted repository state.

## Cross-instance restore

To restore one instance's snapshot into another:

```yaml
spec:
  restoreFrom: "<source-snapshot-key>"
```

The operator does **not** rewrite `meta.json.instance_uid`. The hermes-agent runtime will see a new UID on the running instance and treat the imported data as foreign. This is intentional: if you don't want that, do the restore manually by copying the object, extracting the archive, and editing metadata before starting the agent.

## Format evolution

When `format_version` is bumped:

- Old snapshots remain restorable (backward compatibility is a v1.x stability commitment).
- The operator's init container at runtime version N can read all `format_version` <= N.
- Cross-version downgrades (newer snapshot, older operator) are unsupported.
