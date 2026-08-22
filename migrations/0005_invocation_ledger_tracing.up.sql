-- 阶段 3（设计 §10.1）：扩展成本总账，支持 Trace 关联与契约验收口径。
-- 旧行由列默认值覆盖：outcome='accepted'、cost_quality='estimated'，
-- trace_id/provider_request_index/ttft_ms/finish_reason/error_code 为 NULL。
ALTER TABLE agent_model_invocations
    ADD COLUMN trace_id VARCHAR(32) NULL,
    ADD COLUMN provider_request_index INT UNSIGNED NULL,
    ADD COLUMN outcome VARCHAR(32) NOT NULL DEFAULT 'accepted',
    ADD COLUMN cost_quality VARCHAR(16) NOT NULL DEFAULT 'estimated',
    ADD COLUMN ttft_ms BIGINT UNSIGNED NULL,
    ADD COLUMN finish_reason VARCHAR(32) NULL,
    ADD COLUMN error_code VARCHAR(64) NULL;
