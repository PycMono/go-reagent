ALTER TABLE agent_model_invocations
    DROP COLUMN cache_write_price_usd_per_million_tokens,
    DROP COLUMN cache_read_price_usd_per_million_tokens,
    DROP COLUMN reasoning_tokens,
    DROP COLUMN cache_write_tokens,
    DROP COLUMN cache_read_tokens;
