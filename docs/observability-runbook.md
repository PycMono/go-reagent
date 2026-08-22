# go-reagent 可观测性 Runbook（设计 §20 阶段 5）

## 关联路径

```text
Metric → Exemplar → Trace → Related Logs → Run Ledger
```

1. Grafana 告警/面板发现异常指标（含 Exemplar 时点按 trace_id 跳转 Tempo）。
2. Tempo 中按 `trace_id` 查看完整 Span 树；从 Ledger 反查时先读
   `agent_model_invocations.trace_id`，再用 `reagent.provider.request_index`
   属性在单条 Trace 内定位唯一 Provider Span（需要 Trace 后端支持属性过滤）。
3. 结构化日志的 `trace_id`/`span_id` 由 go-logger-sdk 自动注入；
   `run_id` 用于关联 `agent_messages` / `agent_model_invocations`。
4. Trace 默认保留 7 天，Metrics 30 天，Ledger 沿用 Conversation 策略；
   过期 Trace 不可恢复，长期汇总以 Ledger 为准。

## 关键指标口径

- `reagent.model.requests` 统计**物理请求**（含失败/重试）；`outcome` 区分
  `success|error|canceled|deadline_exceeded`，`error_code` 无错误时为 `none`。
- `reagent.model.invocations` 只统计**可信 Usage**；`acceptance` 区分
  `accepted|contract_invalid`（契约非法但已计费的调用）。
- `reagent.model.tokens` 的 `cache_read/cache_write/reasoning` 是
  `input_total/output_total` 的子集，**不能全部求和**。
- `reagent.model.cost` 按 `cost_quality` 区分 `exact|estimated`；
  精确成本报表只使用 `exact`。
- TTFT 三处同源（Span `reagent.stream.ttft_ms`、Histogram、Ledger `ttft_ms`）；
  纯 Tool Call 为缺省/NULL，已观测但不足 1ms 为 0。

## 首次部署后必做

- 打开 `http://<host>:9464/metrics` 确认 Prometheus 序列名（尤其
  `reagent_model_cost_*` 的 USD 单位后缀与 Histogram 的 `_seconds` 后缀），
  如与 `prometheus-rules.yaml`、Dashboard JSON 中的名称不符，同步修正。
- 验证 Exemplar：采样请求后 Histogram 数据点应携带 trace_id。
- 验证告警最小样本条件在低流量环境不误报。

## 故障处置

| 症状 | 处置 |
|---|---|
| `ReagentCollectorDroppingSpans` | 检查 Collector 与 Tempo 容量/网络；应用侧队列有界且 Fail-open，业务不受影响 |
| `ReagentMetricsScrapeFailing` | 检查应用 9464 端口监听与 NetworkPolicy |
| `ReagentRateLimitedRatioHigh` | 检查 Provider 配额；Retry 已有退避，必要时降流 |
| `ReagentContextOverflowRatioHigh` | 检查上下文窗口配置与 Compaction 是否生效（`reagent.compactions`） |
| `ReagentHourlyCostOverBudget` | 按 `provider/model/phase` 分解成本；Ledger 与 Provider 账单对账 |

## Collector 不可达

应用行为不变（Fail-open）：Span 经有界队列丢弃，错误日志限频。
恢复 Collector 后无需重启应用。

## 采样切换

- Head（默认）：`sampling_mode=head`，`sample_ratio` 从 1.0 开始，容量验证
  后才可降低；Head 无法按最终成本/结果补采。
- Tail：`sampling_mode=tail` 且 `sample_ratio=1.0`，Collector 使用
  `otel-collector-tail.yaml` 的策略（异常/高成本 100% 保留，普通成功 10%）。
