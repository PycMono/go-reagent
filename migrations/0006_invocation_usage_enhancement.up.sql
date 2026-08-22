-- 阶段 4（设计 §10.1）：Usage 增强，缓存/推理分项 Token 与缓存价格。
-- 新增 Token 字段由无符号类型保证非负，价格仍执行 §9.1 校验。
ALTER TABLE agent_model_invocations
    ADD COLUMN cache_read_tokens BIGINT UNSIGNED NOT NULL DEFAULT 0,
    ADD COLUMN cache_write_tokens BIGINT UNSIGNED NOT NULL DEFAULT 0,
    ADD COLUMN reasoning_tokens BIGINT UNSIGNED NOT NULL DEFAULT 0,
    ADD COLUMN cache_read_price_usd_per_million_tokens DECIMAL(20,12) NOT NULL DEFAULT 0,
    ADD COLUMN cache_write_price_usd_per_million_tokens DECIMAL(20,12) NOT NULL DEFAULT 0;
