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

## 排查入口

Grafana Explore 选择 Tempo 查看运行、模型请求、工具执行和压缩的 Trace。
模型用量、成本与调用结果保存在调用账本中，用于预算控制和对账。

应用不再上报 Agent、模型、工具、压缩和护栏的自定义 Metrics，相关看板和业务指标告警已移除。
Prometheus 保留 SDK 的进程、HTTP 等基础指标；告警规则保留 `reagent.pipeline` 组，监测采集链路。

注意：容器内 Prometheus 抓取宿主机应用时使用 `host.docker.internal:9464`。
