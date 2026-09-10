# Task 5 report: tenant-owned conversation persistence

## Delivered

- Extended persisted conversations with tenant, Agent/version binding, conversation type, and follow-latest fields while retaining `profile_code` and all existing repository method signatures.
- Added `IAgentConversationRepository` with `CreateBound` and `CommitAgentVersion`. Bound creation locks the tenant Agent, verifies it is enabled and the requested version is active, then inserts the chat binding transactionally. Version admission locks the Agent first, rechecks enabled/current-version state, then locks and CAS-updates the owned chat binding. Fixed-version conversations retain their stored version.
- Scoped every existing repository path when a verified `identity.Principal` is present: tenant, exact-case user/public conversation ID (`BINARY` for legacy case-insensitive columns), and `conversation_type='chat'`. This includes find, list, message reads, append, rename, rename-if-untitled, and delete. Passed user IDs must exactly match the Principal.
- Preserved CLI/local behavior when no Principal is present, including legacy lazy conversation creation and legacy inserts that omit the new nullable/defaulted columns.
- Updated `conversation.Runner` so a Principal-scoped Web run rejects a mismatched user and returns NotFound for a missing conversation without recreating it. The existing constructor and unscoped lazy-create behavior remain unchanged.
- Added Agent filtering to management list queries and ensured normal chat paths never expose training conversations or their messages, including reads by internal conversation ID.

## Tests

- Added sqlmock tests for tenant/exact-owner/type scoping, internal-ID history joins, and Principal mismatch rejection.
- Added Runner tests proving Principal-scoped missing conversations are not created and user mismatch stops before repository access.
- Added a disposable MySQL integration test covering two tenants with the same user/public conversation ID, case-only attacks, cross-tenant history/append/rename/delete, training-row exclusion, binding mismatch, Agent version admission, and explicit `follow_latest=false` persistence.
- The duplicate public-ID fixture explicitly models the post-0008 owner index by replacing the retained 0007 legacy owner index only inside its disposable schema. Production migration 0007 remains additive and unchanged.
- Updated the pre-existing integration test for the current `NewIDService` constructor and the Fx graph test with explicit anonymous identity configuration.

## Verification

- `GOMODCACHE=/tmp/go-reagent-employees-modcache GOCACHE=/tmp/go-reagent-phase1a-cache go test ./conversation ./infrastructure/persistence/conversation -count=1` — PASS.
- `go test -tags=integration ./infrastructure/persistence/conversation -run TestMySQLTenantOwnedConversations -count=1 -v` with the same caches against isolated MySQL 8.4.11 on `127.0.0.1:59755` — PASS. The test created and removed a timestamped schema.
- `git diff --check` — PASS.

## Integration note

`infrastructure/persistence/register.go` must expose the concrete repository as `conversationrepo.IAgentConversationRepository` for Web service injection. The parent integration task owns that shared registration file.
