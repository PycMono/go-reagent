# Task 3 implementation report

Implemented the immutable version snapshot, RFC 8785 digest protocol, Agent Bundle port, Git-backed initial snapshots, commit verification, and immutable version/chat materialization.

The schema freezes model protocol, endpoint, model, pricing, capabilities and server-owned `SecretRef` without credentials. It freezes exact builtin/registered/MCP implementation references, permission rules, effective runtime limits, loop detection, compaction, history, process/write flags and workspace policy. `DecodeStrict` rejects oversized documents, malformed/trailing JSON, duplicate keys, nulls and unknown fields. `ParseSnapshot` additionally enforces every v1 field, rejects unsupported reasoning, validates pricing and requires runtime permission flags to match builtin tools.

Bundle storage uses `tenants/<tenant>/agents/<agent>/bundle.git`, `versions/<version>`, and `runtime-cache/chat/<conversation>/<version>`. IDs are validated as safe single path components. Source inspection includes ignored files, excludes only Git metadata, rejects dirty Git sources, reserved runtime paths, escaping or broken links, special files, multiple hard links and configured size/path limits. Commit verification reads every blob, reconstructs and inspects the workspace, resolves symlink chains and recomputes the content digest. Git object IDs are recorded separately from the JCS/SHA-256 Bundle digest.

Materialization copies blobs into a temporary sibling directory without hard links, validates before rename, and makes behavior files/directories read-only. Chat copies receive separate writable `scratch` and `.tmp` directories. Existing destinations are reverified and are retained on any failure.

## Evidence

- RED: `GOPATH=/tmp/go-reagent-employees-gopath GOMODCACHE=/tmp/go-reagent-employees-modcache GOCACHE=/tmp/go-reagent-phase1a-cache go test ./application/service/agentversion ./infrastructure/driver/agentbundle -count=1` failed with undefined `ParseSnapshot`, `BundleDigest`, `SpecDigest`, `Store`, `New`, and `BundleRef`.
- GREEN: `GOPATH=/tmp/go-reagent-employees-gopath GOMODCACHE=/tmp/go-reagent-employees-modcache GOCACHE=/tmp/go-reagent-phase1a-cache go test ./application/service/agentversion ./infrastructure/driver/agentbundle ./config -count=1` passed: agentversion `1.033s`, agentbundle `2.169s`, config `1.475s`.
- Native macOS: the first run exposed a `/var` versus `/private/var` fixture alias in the payload HOME contract. After canonicalizing the fixture, `RUN_WORKSPACE_SANDBOX_INTEGRATION=1 ... go test ./infrastructure/driver/agentbundle -run Native -count=1 -v` passed `TestNativeChatCannotReadSiblingScratch`; package `0.579s`.
- `go vet ./application/service/agentversion ./infrastructure/driver/agentbundle ./config` passed.
- Final fresh package run passed: agentversion `0.369s`, agentbundle `1.143s`, config `1.154s`.
- Race run passed: agentversion `1.583s`, agentbundle `2.599s`.
- Final frozen-source macOS native run passed `TestNativeChatCannotReadSiblingScratch`; package `1.053s`.
- `git diff --check` passed.

The pinned JCS implementation is `github.com/cyberphone/json-canonicalization` at `v0.0.0-20241213102144-19d51d7fe467`, licensed Apache-2.0. Its RFC number and Unicode ordering behavior are covered by fixed canonical-byte and digest fixtures.

Linux Bubblewrap native verification remains for the parent using the frozen source. No publication endpoint or training checkpoint behavior was added.

## Review fix round 1

Aligned the digest wire exactly with the master protocol: per-entry `content_sha256` is now 64 lowercase hexadecimal characters without a prefix, while aggregate digests retain `sha256:`. The fixed manifest fixture now expects `sha256:e159b3c1fa68d9942d8aff1778399c9cce45aad72e44ce6e2a15c85f424b6425`. Spec payload keys are exactly `model_config`, `tool_policy`, and `runtime_config`; the complete independent canonical-byte fixture expects `sha256:1f7129dbf7fc286b5aec648c210c14cf39c323948ea12a2941206fdb6b587dba`.

Bundle validation now enforces the fixed §5.1 paths and §7.3 asset rules. Tests reject arbitrary root files, nested Git metadata, `.gitmodules`, `.gitattributes`, platform config, ELF/PE/Mach-O binaries, misplaced scripts, and executables outside Skill script directories. Checked files are staged by their exact paths instead of `git add -A`; `.gitignore` cannot silently omit an inspected file.

Git blob size is checked with `cat-file -s` before content allocation and content is read through a `max+1` bounded stream. Existing materialized regular files are stat-checked, bounded while reading, identity-checked, and rejected when multiply hardlinked. Real oversized Git blob, oversized materialized file, and materialized hardlink fixtures cover these boundaries.

- Review RED: digest tests failed on the prefixed entry format and missing exact SpecDigest helper; seven fixed-tree/native/script cases were accepted by the old driver.
- Review GREEN: agentversion `0.366s`, agentbundle `1.744s`, config `1.121s`.
- Review race: agentversion `2.178s`, agentbundle `2.646s`.
- Review vet and `git diff --check` passed.
- Review macOS native: `TestNativeChatCannotReadSiblingScratch` passed; package `1.130s`.

## Review fix round 2

Existing materializations now revalidate directory permissions as well as file content and modes. The WorkDir root and every behavior directory must remain exactly `0555`; chat `scratch` and `.tmp` must remain actual `0700` directories, and only their descendants are excluded from immutable content comparison. Regression fixtures mutate the root, a behavior subdirectory, and scratch permissions and confirm reuse fails closed. `go test ./infrastructure/driver/agentbundle -count=1` passed in `2.309s`.
