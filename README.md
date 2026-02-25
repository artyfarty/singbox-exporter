## singbox-exporter

Prometheus exporter for [sing-box](https://sing-box.sagernet.org/) proxy. Vibe-coded fork of [zxh326/clash-exporter](https://github.com/zxh326/clash-exporter), adapted for sing-box's Clash-compatible API.

All metrics keep the `clash_` namespace prefix for backward compatibility with existing Grafana dashboards.

### What this fork adds

The upstream clash-exporter has no outbound/proxy state metrics. This fork adds a collector that polls sing-box's `/proxies` endpoint and exposes:

- **`clash_outbound_up`** — whether each outbound is alive (1), down (0), or never tested (-1)
- **`clash_outbound_delay_ms`** — last measured latency per outbound
- **`clash_outbound_group_info`** — group metadata: members, currently selected outbound

Delay data is read passively from sing-box's history (populated automatically for URLTest groups). No active delay tests are triggered.

### Docker

```
docker pull ghcr.io/artyfarty/singbox-exporter:main
```

docker-compose example:

```yaml
  singbox-exporter:
    image: ghcr.io/artyfarty/singbox-exporter:main
    container_name: singbox-exporter
    entrypoint: ["/app/clash-exporter", "-collectDest=true"]
    environment:
      - CLASH_HOST=<singbox-ip>:9090
      - CLASH_TOKEN=
    restart: always
    ports:
      - 2112:2112
```

`CLASH_HOST` should point to sing-box's `external_controller` address.

### Build from source

```sh
go build -o singbox-exporter .
CLASH_HOST=<singbox-ip>:9090 ./singbox-exporter -port 2112
```

### Usage

```
  -collectDest
        enable per-destination traffic metrics (default true)
        Warning: generates a large number of metrics
  -collectTracing
        enable tracing metrics (default false)
        Note: sing-box currently stubs this endpoint (404)
  -port int
        port to listen on (default 2112)
```

### Metrics

| Metric | Type | Labels |
|--------|------|--------|
| `clash_info` | Gauge | `version`, `premium` |
| `clash_download_bytes_total` | Gauge | |
| `clash_upload_bytes_total` | Gauge | |
| `clash_active_connections` | Gauge | |
| `clash_network_traffic_bytes_total` | Counter | `source`, `destination`, `policy`, `type` |
| `clash_outbound_up` | Gauge | `name`, `type`, `group` |
| `clash_outbound_delay_ms` | Gauge | `name`, `type`, `group` |
| `clash_outbound_group_info` | Gauge | `name`, `type`, `now`, `members` |

### Prometheus config

```yaml
- job_name: "singbox"
  metrics_path: /metrics
  scrape_interval: 1s
  static_configs:
    - targets: ["127.0.0.1:2112"]
```

### Grafana

Import [dashboard.json](./grafana/dashboard.json) or use Grafana dashboard ID `18530` as a starting point.

