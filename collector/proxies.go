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
	Type    string            `json:"type"`
	Name    string            `json:"name"`
	UDP     bool              `json:"udp"`
	History []delayHistory    `json:"history"`
	All     []string          `json:"all"`
	Now     string            `json:"now"`
}

type delayHistory struct {
	Time  string `json:"time"`
	Delay int    `json:"delay"`
}

type proxiesResponse struct {
	Proxies map[string]proxyInfo `json:"proxies"`
}

var (
	outboundUp            *prometheus.GaugeVec
	outboundDelayMs       *prometheus.GaugeVec
	outboundGroupInfo     *prometheus.GaugeVec
	outboundGroupSelected *prometheus.GaugeVec
)

type Proxies struct{}

func (*Proxies) Name() string {
	return "proxies"
}

func (*Proxies) Collect(config CollectConfig) error {
	log.Println("starting collector: proxies")
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	// collect immediately, then on each tick
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

	// Reset to clear stale series
	outboundUp.Reset()
	outboundDelayMs.Reset()
	outboundGroupInfo.Reset()
	outboundGroupSelected.Reset()

	for name, info := range result.Proxies {
		// Determine up status and delay from history
		var up float64 = -1 // never tested
		var delay float64

		if len(info.History) > 0 {
			last := info.History[len(info.History)-1]
			delay = float64(last.Delay)
			if last.Delay > 0 {
				up = 1
			} else {
				up = 0
			}
		}

		// Emit per-group membership for leaf outbounds
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

func init() {
	outboundUp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "clash",
			Name:      "outbound_up",
			Help:      "Whether the outbound is up (1), down (0), or never tested (-1), based on last delay test.",
		},
		[]string{"name", "type", "group"},
	)

	outboundDelayMs = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "clash",
			Name:      "outbound_delay_ms",
			Help:      "Last measured delay in milliseconds for the outbound. 0 if failed or untested.",
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
			Help:      "Whether each member is the currently selected outbound in its group. 1=selected, 0=not selected.",
		},
		[]string{"group", "name"},
	)

	prometheus.MustRegister(outboundUp, outboundDelayMs, outboundGroupInfo, outboundGroupSelected)
	Register(new(Proxies))
}
