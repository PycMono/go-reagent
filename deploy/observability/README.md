# 本地观测栈示例（设计 §20 阶段 1 交付）

一键启动 OTLP Collector → Tempo（Trace）、Prometheus（Metrics）、Grafana：

```bash
docker compose -f deploy/observability/docker-compose.yaml up -d
```

```bash
docker compose -f deploy/observability/docker-compose.yaml ps    # 确认 4 个容器 running
```

然后启用应用可观测性（`config.json`）：

```json
"observability": {
  "enabled": true,
  "service_name": "go-reagent",
  "environment": "development",
  "otlp": {"endpoint": "http://127.0.0.1:4317", "protocol": "grpc", "insecure": true},
  "tracing": {"enabled": true, "sampling_mode": "head", "sample_ratio": 1.0},
  "metrics": {"enabled": true, "host": "127.0.0.1", "port": 9464, "path": "/metrics", "runtime_metrics": true},
  "content": {"mode": "none"}
}
```

- Grafana: http://127.0.0.1:3000 （admin/admin，已预置 Prometheus/Tempo 数据源）
- Prometheus: http://127.0.0.1:9090 （抓取应用 `127.0.0.1:9464/metrics`）
- 应用 Trace 经 OTLP/gRPC 4317 进入 Collector，再写入 Tempo

## 看板

Grafana 左侧 Dashboards → go-reagent 目录，或直接访问：

| 看板 | 地址 | 内容 |
|---|---|---|
| Agent | http://127.0.0.1:3000/d/reagent-agent | Run 数/终止原因、P50/P95/P99 时延、每 Run 成本、Turn/Invocation 分布 |
| Model | http://127.0.0.1:3000/d/reagent-model | 请求/错误率、P95 时延与 TTFT、Token 分类、成本（CostQuality）、Retry/Overflow、缓存命中率 |
| Tool | http://127.0.0.1:3000/d/reagent-tool | 调用与错误率、P95 执行/排队时延、稳定错误码 |

告警规则见 Prometheus → Alerts（`reagent.agent` / `reagent.pipeline` 两组）。

注意：容器内 Prometheus 抓取宿主机应用时使用 `host.docker.internal:9464`。
