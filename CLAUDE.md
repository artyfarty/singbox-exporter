# singbox-exporter

Prometheus exporter for [sing-box](https://sing-box.sagernet.org/) proxy. Forked from [zxh326/clash-exporter](https://github.com/zxh326/clash-exporter) and adapted for sing-box specific metrics.

## Build & Run

```bash
go build -o singbox-exporter .
CLASH_HOST=<singbox-ip>:9090 ./singbox-exporter -port 2112
# or via Docker: see docker-compose.yml
```

Test metrics: `curl localhost:2112/metrics | grep clash_`

## Architecture

Self-registering collector pattern. Each collector in `collector/` calls `Register()` in its `init()` and runs in its own goroutine with automatic retry + exponential backoff (see `collector/collector.go:Start()`).

### Collectors

| File | Collector | Data Source | Pattern |
|------|-----------|-------------|---------|
| `collector/info.go` | Info | `GET /version` | One-shot HTTP request |
| `collector/connections.go` | Connection | `ws:///connections` | Persistent WebSocket stream |
| `collector/tracing.go` | Tracing | `ws:///profile/tracing` | Persistent WebSocket stream (opt-in via `-collectTracing`) |
| `collector/proxies.go` | Proxies | `GET /proxies` | HTTP polling every 15s |

### Adding a new collector

1. Create `collector/<name>.go`
2. Implement the `Collector` interface: `Name() string` and `Collect(config CollectConfig) error`
3. In `init()`: create prometheus metrics with `prometheus.NewGaugeVec(...)`, call `prometheus.MustRegister(...)`, call `Register(new(YourCollector))`
4. The collector auto-registers — no changes needed in `main.go`

### Config

`CollectConfig` (in `collector/collector.go`) carries all runtime config. Flags are in `main.go`, env vars: `CLASH_HOST`, `CLASH_TOKEN`.

## Sing-box Clash API Reference

Sing-box exposes a Clash-compatible API at the `external_controller` address. Auth via `Authorization: Bearer {secret}` header.

### Key endpoints for this exporter

| Endpoint | Method | Used by | Notes |
|----------|--------|---------|-------|
| `/version` | GET | info.go | `{"version": "sing-box 1.12.x", "meta": true, "premium": true}` |
| `/connections` | WS | connections.go | Streams `{downloadTotal, uploadTotal, connections[]}` |
| `/proxies` | GET | proxies.go | Returns all outbounds: groups + leaves. See below. |
| `/group` | GET | (unused) | Returns only groups. Subset of `/proxies`. |
| `/group/{name}/delay?url=...&timeout=ms` | GET | (unused) | Triggers active delay test for all group members. Returns `{"member": delay_ms}`. |
| `/proxies/{name}/delay?url=...&timeout=ms` | GET | (unused) | Tests single proxy delay. |
| `/profile/tracing` | WS | tracing.go | Stubbed (404) in current sing-box. |
| `/memory` | GET/WS | (unused) | `{"inuse": bytes, "oslimit": 0}` |

### GET /proxies response structure

```json
{
  "proxies": {
    "<name>": {
      "type": "Shadowsocks|WireGuard|Awg|Selector|URLTest|Fallback|Direct|...",
      "name": "<outbound tag>",
      "udp": true,
      "history": [{"time": "RFC3339", "delay": 246}],
      "all": ["member1", "member2"],  // groups only
      "now": "member1"                // groups only
    }
  }
}
```

- `history` is transient — only populated for outbounds in URLTest groups or after manual delay tests
- `delay > 0` means alive, `delay == 0` means test failed, empty `history` means never tested
- Group types seen in production: Selector, URLTest, Fallback
- Leaf types seen: Shadowsocks, WireGuard, Awg, Direct

## Metrics

All metrics use the `clash_` namespace prefix (kept for backward compat with existing dashboards).

### Outbound metrics (proxies.go)

| Metric | Labels | Description |
|--------|--------|-------------|
| `clash_outbound_up` | name, type, group | 1=alive, 0=down, -1=never tested |
| `clash_outbound_delay_ms` | name, type, group | Last delay in ms |
| `clash_outbound_group_info` | name, type, now, members | Group metadata (always 1) |
| `clash_outbound_group_selected` | group, name | 1=selected, 0=not selected per group member |

The `group` label on `up`/`delay_ms` means one series per group membership. A proxy in 3 groups gets 3 series.

### Traffic metrics (connections.go)

| Metric | Labels |
|--------|--------|
| `clash_upload_bytes_total` | — |
| `clash_download_bytes_total` | — |
| `clash_active_connections` | — |
| `clash_network_traffic_bytes_total` | source, destination, policy, type |

### Other

| Metric | Labels |
|--------|--------|
| `clash_info` | version, premium |
| `clash_tracing_*` | (tracing histograms, requires `-collectTracing`) |
