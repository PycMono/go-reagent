# Model Cost Observability Design (Superseded)

This original design used numeric internal keys and persistence-private row models. Those decisions were replaced by the database/business boundary refactor.

The current design is documented in:

- `docs/conversation-persistence.md`
- `docs/superpowers/plans/2026-08-13-database-layer-boundaries.md`

The authoritative implementation uses string Snowflake IDs, Domain table entities, `conversation_id` string foreign keys, and `agent_model_invocations` as the sole Token/cost ledger.
