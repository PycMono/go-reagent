# go-reagent 可观测性 Runbook（设计 §20 阶段 5）

## 关联路径

```text
Run Ledger ↔ Trace ↔ Related Logs
```

1. 从运行结果或日志发现失败、预算触顶等情况。
2. 从 `agent_model_invocations.trace_id` 查找 Tempo Trace，再用
   `reagent.provider.request_index` 属性定位 Provider Span。
3. 结构化日志自动携带 `trace_id`/`span_id`，`run_id` 用于关联消息与调用账本。
4. Trace 默认保留 7 天，基础 Metrics 30 天；长期用量和成本汇总以账本为准。

## 用量和成本

Agent、模型、工具、压缩和护栏不再上报自定义 Metrics，也不再提供相关业务看板和指标告警。
预算控制使用 Governor 的运行累计值，持久化使用调用账本，不依赖 Metrics。

- 账本中的 `accepted|contract_invalid` 区分调用契约结果，非法回复产生的可信用量仍入账。
- 缓存和推理 Token 是输入/输出总量的子集，不能与总量重复相加。
- 成本按 `cost_quality` 区分 `exact|estimated`，精确报表只使用 `exact`。
- TTFT 保留在 Span 的 `reagent.stream.ttft_ms` 和账本的 `ttft_ms` 中；
  纯工具调用为缺省/NULL，已观测但不足 1ms 为 0。

## 部署验证

- 检查应用 `/metrics` 端点及 Prometheus 抓取状态；该端点用于 SDK 的进程、HTTP 等基础指标。
- 执行一次请求，验证调用账本与运行汇总一致，且账本的 trace_id 能关联 Tempo。
- 业务费用和用量阈值由运行预算控制；需要跨运行成本分析时查询账本。

## 故障处置

| 症状 | 处置 |
|---|---|
| `ReagentCollectorDroppingSpans` | 检查 Collector 与 Tempo 容量/网络；应用侧队列有界且 Fail-open，业务不受影响 |
| `ReagentMetricsScrapeFailing` | 检查应用 9464 端口监听与 NetworkPolicy |
| 模型限流 | 检查 Provider 配额；Retry 已有退避，必要时降流 |
| 上下文超限 | 检查上下文窗口配置与 压缩是否生效（查看压缩 Span） |
| 模型成本偏高 | 按账本中的平台、模型和阶段汇总成本，与 Provider 账单对账 |

## Collector 不可达

应用行为不变（Fail-open）：Span 经有界队列丢弃，错误日志限频。
恢复 Collector 后无需重启应用。

## 采样切换

- Head（默认）：`sampling_mode=head`，`sample_ratio` 从 1.0 开始，容量验证
  后才可降低；Head 无法按最终成本/结果补采。
- Tail：`sampling_mode=tail` 且 `sample_ratio=1.0`，Collector 使用
  `otel-collector-tail.yaml` 的策略（异常/高成本 100% 保留，普通成功 10%）。
