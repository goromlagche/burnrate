package main

import "strings"

// USD per million tokens. Cache writes cost 1.25x input (5m TTL) or 2x input (1h TTL).
type price struct {
	In, Out, CacheRead float64
}

// Matched by prefix, longest first, so "claude-opus-5-5" wins over "claude-opus-5".
var prices = []struct {
	prefix string
	p      price
}{
	{"claude-fable-5-1", price{10, 50, 0.25}},
	{"claude-mythos-5-1", price{10, 50, 0.25}},
	{"claude-fable-5", price{10, 50, 1}},
	{"claude-mythos-5", price{10, 50, 1}},
	{"claude-opus-5-5", price{4, 20, 0.20}},
	{"claude-opus-5", price{5, 25, 0.50}},
	{"claude-opus-4", price{5, 25, 0.50}},
	{"claude-sonnet-5-5", price{2, 10, 0.10}},
	{"claude-sonnet-5", price{2, 10, 0.20}},
	{"claude-sonnet-4", price{3, 15, 0.30}},
	{"claude-haiku-5-5", price{0.10, 0.50, 0.01}},
	{"claude-haiku-4", price{1, 5, 0.10}},
}

type claudeUsage struct {
	Input         int64 `json:"input_tokens"`
	Output        int64 `json:"output_tokens"`
	CacheCreate   int64 `json:"cache_creation_input_tokens"`
	CacheRead     int64 `json:"cache_read_input_tokens"`
	CacheCreation *struct {
		Write5m int64 `json:"ephemeral_5m_input_tokens"`
		Write1h int64 `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
	Speed string `json:"speed"`
}

// claudePrice finds the rates by prefix. Unknown models, and the <synthetic>
// placeholders that never reached the API, stay unpriced.
func claudePrice(model string) (price, bool) {
	for _, e := range prices {
		if strings.HasPrefix(model, e.prefix) {
			return e.p, true
		}
	}
	return price{}, false
}

// cost estimates the API list price of one response.
func (u *claudeUsage) cost(model string) float64 {
	p, ok := claudePrice(model)
	if !ok {
		return 0
	}
	// Haiku 5.5 has a higher tier for prompts over 100K tokens.
	if strings.HasPrefix(model, "claude-haiku-5-5") && u.Input+u.CacheCreate+u.CacheRead > 100_000 {
		p = price{0.50, 2.50, 0.05}
	}
	if u.Speed == "fast" {
		p = price{p.In * 2, p.Out * 2, p.CacheRead * 2}
	}

	w5m, w1h := u.CacheCreate, int64(0)
	if c := u.CacheCreation; c != nil && c.Write5m+c.Write1h > 0 {
		w5m, w1h = c.Write5m, c.Write1h
	}
	usd := float64(u.Input)*p.In +
		float64(u.Output)*p.Out +
		float64(u.CacheRead)*p.CacheRead +
		float64(w5m)*p.In*1.25 +
		float64(w1h)*p.In*2
	return usd / 1e6
}

// OpenAI per-million prices, from developers.openai.com/api/docs/pricing (2026-10-25).
// Short context (<=272K input); long context doubles input and adds 50% to output.
type codexPrice struct {
	In, Cached, Write, Out float64 // Write = 30m cache write
}

var codexPrices = map[string]codexPrice{
	"gpt-6-astra":   {10, 1, 12.5, 50},
	"gpt-6.1-sol":   {2, 0.10, 2.5, 10},
	"gpt-6-sol":     {2, 0.20, 2.5, 10},
	"gpt-6-luna":    {0.10, 0.01, 0.125, 0.50},
	"gpt-5.6-sol":   {4, 0.40, 5, 20},
	"gpt-5.6-terra": {2, 0.20, 2.5, 12},
	"gpt-5.6-luna":  {0.20, 0.02, 0.25, 1.20},
	"gpt-5.3-codex": {1.75, 0.175, 1.75, 14},
}

type codexUsage struct {
	Input      int64 `json:"input_tokens"` // includes cached and cache-write tokens
	Cached     int64 `json:"cached_input_tokens"`
	CacheWrite int64 `json:"cache_write_input_tokens"`
	Output     int64 `json:"output_tokens"` // includes reasoning tokens
}

// cost is zero for unpriced models such as codex-auto-review.
// The priority (fast) tier is 2x every rate.
func (u codexUsage) cost(model string, priority bool) float64 {
	p := codexPrices[model]
	if u.Input > 272_000 {
		p = codexPrice{p.In * 2, p.Cached * 2, p.Write * 2, p.Out * 1.5}
	}
	if priority {
		p = codexPrice{p.In * 2, p.Cached * 2, p.Write * 2, p.Out * 2}
	}
	uncached := max(u.Input-u.Cached-u.CacheWrite, 0)
	usd := float64(uncached)*p.In +
		float64(u.Cached)*p.Cached +
		float64(u.CacheWrite)*p.Write +
		float64(u.Output)*p.Out
	return usd / 1e6
}
