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
