ALTER TABLE agent_model_invocations
    DROP COLUMN error_code,
    DROP COLUMN finish_reason,
    DROP COLUMN ttft_ms,
    DROP COLUMN cost_quality,
    DROP COLUMN outcome,
    DROP COLUMN provider_request_index,
    DROP COLUMN trace_id;
