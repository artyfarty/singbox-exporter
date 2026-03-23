package collector

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type proxyInfo struct {
	Type    string         `json:"type"`
	Name    string         `json:"name"`
	UDP     bool           `json:"udp"`
	History []delayHistory `json:"history"`
	All     []string       `json:"all"`
	Now     string         `json:"now"`
}

type delayHistory struct {
	Time  string `json:"time"`
	Delay int    `json:"delay"`
}

type proxiesResponse struct {
	Proxies map[string]proxyInfo `json:"proxies"`
}

type delayResponse struct {
	Delay   int    `json:"delay"`
	Message string `json:"message"`
}

var (
	outboundUp            *prometheus.GaugeVec
	outboundDelayMs       *prometheus.GaugeVec
	outboundGroupInfo     *prometheus.GaugeVec
	outboundGroupSelected *prometheus.GaugeVec
)

const (
	probeURL     = "https://www.gstatic.com/generate_204"
	probeTimeout = 1500 // ms
)

type Proxies struct{}

func (*Proxies) Name() string {
	return "proxies"
}

func (*Proxies) Collect(config CollectConfig) error {
	log.Println("starting collector: proxies")
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	if err := collectProxies(config); err != nil {
		return err
	}
	for range ticker.C {
		if err := collectProxies(config); err != nil {
			log.Println("proxies: poll error:", err)
		}
	}
	return nil
}

func collectProxies(config CollectConfig) error {
	endpoint := fmt.Sprintf("http://%s/proxies", config.ClashHost)
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return err
	}
	if config.ClashToken != "" {
		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", config.ClashToken))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var result proxiesResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return err
	}

	// Build reverse map: proxy name -> list of groups it belongs to
	groupMembership := make(map[string][]string)
	for groupName, info := range result.Proxies {
		if len(info.All) == 0 {
			continue
		}
		for _, member := range info.All {
			groupMembership[member] = append(groupMembership[member], groupName)
		}
	}

	// Active probe leaf proxies sequentially (not groups, not Direct)
	probeResults := make(map[string]int)
	for name, info := range result.Proxies {
		if len(info.All) > 0 || info.Type == "Direct" {
			continue
		}
		probeResults[name] = probeProxy(config, name)
	}

	// Reset to clear stale series
	outboundUp.Reset()
	outboundDelayMs.Reset()
	outboundGroupInfo.Reset()
	outboundGroupSelected.Reset()

	for name, info := range result.Proxies {
		var up float64
		var delay float64

		if probeDelay, ok := probeResults[name]; ok {
			if probeDelay > 0 {
				up = 1
				delay = float64(probeDelay)
			} else {
				delay = 99999
			}
		} else if len(info.All) == 0 {
			// Direct or other non-probed leaf — skip
			continue
		} else {
			// Group — not probed, skip up/delay
			goto emitGroup
		}

		// Emit per-group membership for leaf outbounds
		{
			groups := groupMembership[name]
			if len(groups) > 0 {
				for _, g := range groups {
					outboundUp.WithLabelValues(name, info.Type, g).Set(up)
					outboundDelayMs.WithLabelValues(name, info.Type, g).Set(delay)
				}
			} else {
				outboundUp.WithLabelValues(name, info.Type, "").Set(up)
				outboundDelayMs.WithLabelValues(name, info.Type, "").Set(delay)
			}
		}

	emitGroup:
		// Emit group info for groups (those with members)
		if len(info.All) > 0 {
			outboundGroupInfo.WithLabelValues(
				name,
				info.Type,
				info.Now,
				strconv.Itoa(len(info.All)),
			).Set(1)

			for _, member := range info.All {
				if member == info.Now {
					outboundGroupSelected.WithLabelValues(name, member).Set(1)
				} else {
					outboundGroupSelected.WithLabelValues(name, member).Set(0)
				}
			}
		}
	}

	return nil
}

// probeProxy tests a single proxy's delay. Returns delay in ms (>0 = alive, 0 = failed/timeout).
func probeProxy(config CollectConfig, name string) int {
	endpoint := fmt.Sprintf("http://%s/proxies/%s/delay?url=%s&timeout=%d",
		config.ClashHost, name, probeURL, probeTimeout)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return 0
	}
	if config.ClashToken != "" {
		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", config.ClashToken))
	}

	client := &http.Client{Timeout: time.Duration(probeTimeout+1000) * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0
	}

	var dr delayResponse
	if err := json.Unmarshal(body, &dr); err != nil {
		return 0
	}

	return dr.Delay
}

func init() {
	outboundUp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "clash",
			Name:      "outbound_up",
			Help:      "Whether the outbound is up (1) or down (0), based on active delay probe.",
		},
		[]string{"name", "type", "group"},
	)

	outboundDelayMs = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "clash",
			Name:      "outbound_delay_ms",
			Help:      "Last measured delay in milliseconds from active probe. 99999 if failed or timed out.",
		},
		[]string{"name", "type", "group"},
	)

	outboundGroupInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "clash",
			Name:      "outbound_group_info",
			Help:      "Outbound group metadata. Always 1. Labels encode group type, active proxy, and member count.",
		},
		[]string{"name", "type", "now", "members"},
	)

	outboundGroupSelected = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "clash",
			Name:      "outbound_group_selected",
			Help:      "Whether each member is the currently selected outbound for its group (1=selected, 0=not selected).",
		},
		[]string{"group", "name"},
	)

	prometheus.MustRegister(outboundUp, outboundDelayMs, outboundGroupInfo, outboundGroupSelected)
	Register(new(Proxies))
}
