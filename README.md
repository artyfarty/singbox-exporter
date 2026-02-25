## singbox-exporter

Prometheus exporter for [sing-box](https://sing-box.sagernet.org/) proxy. Vibe-coded fork of [zxh326/clash-exporter](https://github.com/zxh326/clash-exporter), adapted for sing-box's Clash-compatible API.

All metrics keep the `clash_` namespace prefix for backward compatibility with existing Grafana dashboards.

### What this fork adds

The upstream clash-exporter has no outbound/proxy state metrics. This fork adds a collector that polls sing-box's `/proxies` endpoint and exposes:

- **`clash_outbound_up`** — whether each outbound is alive (1), down (0), or never tested (-1)
- **`clash_outbound_delay_ms`** — last measured latency per outbound
- **`clash_outbound_group_info`** — group metadata: members, currently selected outbound
- **`clash_outbound_group_selected`** — currently active outbound per group (designed for Grafana State Timeline)

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
| `clash_outbound_group_selected` | Gauge | `group`, `name` |

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

#### State Timeline: active outbound per group

The `clash_outbound_group_selected` metric emits `1` for the currently selected member and `0` for all other members in each Selector/URLTest/Fallback group.

To visualize selection changes over time, add a **State Timeline** panel with:

- **Query**: `clash_outbound_group_selected{group="<your-group>"}`
- **Legend**: `{{name}}`
- **Value mappings**: `1` → "Active", `0` → "Standby"

Minimal State Timeline panel JSON for Grafana:

```json
{
  "type": "state-timeline",
  "title": "Active Outbound per Group",
  "datasource": { "type": "prometheus" },
  "targets": [
    {
      "expr": "clash_outbound_group_selected{group=~\"group1|group2\"}",
      "legendFormat": "{{group}}: {{name}}",
      "refId": "A"
    }
  ],
  "fieldConfig": {
    "defaults": {
      "custom": {
        "fillOpacity": 70,
        "lineWidth": 0
      },
      "mappings": [
        { "type": "value", "options": { "0": { "text": "Standby", "color": "dark-red" } } },
        { "type": "value", "options": { "1": { "text": "Active", "color": "dark-green" } } }
      ]
    }
  },
  "options": {
    "mergeValues": true,
    "showValue": "auto",
    "rowHeight": 0.9
  }
}
```

