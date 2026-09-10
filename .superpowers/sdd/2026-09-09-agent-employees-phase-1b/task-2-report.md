# Task 2 report: Agent catalog persistence

## Delivered

- Added the `agent.Agent`, `agent.Version`, `Presentation`, and `Starter` domain types. Persisted version snapshots and validation evidence remain `json.RawMessage`, keeping the domain independent of application configuration types.
- Added the tenant-scoped Agent repository contract with draft reservation, transactional initial-version commit, immutable version lookup/listing, catalog listing/default selection, bootstrap lookup, and presentation CAS update.
- Added the MySQL repository using the existing `go-mysql-sdk` provider and transaction manager. `CommitInitial` inserts v1 and changes the active pointer in one transaction; a failed pointer CAS rolls back the version row while the separately reserved draft remains available for recovery.
- Added additive migration `0007_agent_catalog`: binary-collated Agent identifiers, immutable versions, scoped foreign keys, nullable conversation ownership, `conversation_type`, and `follow_latest`. The down migration removes the active-version FK before tables and added conversation columns.
- Added sqlmock coverage for tenant-scoped first reads, scoped version reads, transactional rollback, bounded catalog/version limits, independent default selection, and archive CAS with no active training session.
- Added an integration test that creates and removes a unique disposable schema and verifies duplicate bootstrap keys, multiple NULL bootstrap keys, cross-Agent active pointer rejection, duplicate version numbers, duplicate non-NULL source sessions, retained drafts/rolled-back version rows after failed initial CAS, and binary collations.
- Follow-up review fixed management recovery visibility: `IncludeArchived` uses a tenant-scoped LEFT JOIN so drafts without an active version remain visible, while the ordinary catalog still requires an enabled Agent with an active version. Initial-version CAS now also requires the draft to remain enabled, preventing an archived draft from being activated by a delayed recovery path.

## Ownership and commits

The shared checkpoint commit `738151a` already contains the domain types, repository contract/implementation, unit and migration tests, and both 0007 migration files. The Task 2 completion commit contains only the later integration test, archive-CAS unit coverage, and this report. No persistence registration file was changed by Task 2.

## Verification

- Unit command: `GOMODCACHE=/tmp/go-reagent-employees-modcache GOCACHE=/tmp/go-reagent-phase1a-cache go test ./infrastructure/persistence/agent -count=1`
- Integration command: `go test -tags=integration ./infrastructure/persistence/agent -run TestMySQLAgentCatalogConstraintsAndRecovery -count=1 -v` with the same caches and the isolated MySQL connection supplied by the parent task.
- Integration server: MySQL 8.4.11 at the isolated `127.0.0.1:59755` service; each run used a timestamped schema and dropped it during cleanup.

## Limitations

The repository accepts already validated domain input. Strict versioned JSON schema validation, signed catalog cursors, authorization, Git preparation, and dependency registration are owned by later application/integration tasks.
