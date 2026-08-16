package modelscache

// Official OpenCode Go list prices (USD per 1M tokens) and typical
// context windows. Used when GET /models omits those fields.
// Source: https://opencode.ai/docs/go/ (usage limits / prices table).
var catalog = map[string]Info{
	"grok-4.5":          {Context: 500000, InputPerM: 2.00, OutputPerM: 6.00},
	"gpt-5.6-luna":      {Context: 1050000, InputPerM: 0.20, OutputPerM: 1.20},
	"glm-5.3":           {Context: 200000, InputPerM: 1.40, OutputPerM: 4.40},
	"glm-5.2":           {Context: 200000, InputPerM: 1.40, OutputPerM: 4.40},
	"glm-5.1":           {Context: 200000, InputPerM: 1.40, OutputPerM: 4.40},
	"glm-5":             {Context: 200000, InputPerM: 1.40, OutputPerM: 4.40},
	"kimi-k3":           {Context: 256000, InputPerM: 3.00, OutputPerM: 15.00},
	"kimi-k2.7-code":    {Context: 256000, InputPerM: 0.95, OutputPerM: 4.00},
	"kimi-k2.6":         {Context: 256000, InputPerM: 0.95, OutputPerM: 4.00},
	"kimi-k2.5":         {Context: 256000, InputPerM: 0.95, OutputPerM: 4.00},
	"mimo-v2.5":         {Context: 1000000, InputPerM: 0.14, OutputPerM: 0.28},
	"mimo-v2.5-pro":     {Context: 1000000, InputPerM: 0.435, OutputPerM: 0.87},
	"mimo-v2-pro":       {Context: 256000, InputPerM: 0.435, OutputPerM: 0.87},
	"mimo-v2-omni":      {Context: 256000, InputPerM: 0.435, OutputPerM: 0.87},
	"minimax-m3":        {Context: 1000000, InputPerM: 0.30, OutputPerM: 1.20},
	"minimax-m2.7":      {Context: 1000000, InputPerM: 0.30, OutputPerM: 1.20},
	"minimax-m2.5":      {Context: 1000000, InputPerM: 0.30, OutputPerM: 1.20},
	"qwen3.8-max":       {Context: 256000, InputPerM: 2.00, OutputPerM: 6.00},
	"qwen3.7-max":       {Context: 256000, InputPerM: 2.50, OutputPerM: 7.50},
	"qwen3.7-plus":      {Context: 256000, InputPerM: 0.40, OutputPerM: 1.60},
	"qwen3.6-plus":      {Context: 256000, InputPerM: 0.50, OutputPerM: 3.00},
	"qwen3.5-plus":      {Context: 256000, InputPerM: 0.50, OutputPerM: 3.00},
	"deepseek-v4-pro":   {Context: 1000000, InputPerM: 0.435, OutputPerM: 0.87},
	"deepseek-v4-flash": {Context: 1000000, InputPerM: 0.14, OutputPerM: 0.28},
	"hy3":               {Context: 256000, InputPerM: 0.14, OutputPerM: 0.58},
	"hy3-preview":       {Context: 256000, InputPerM: 0.14, OutputPerM: 0.58},
}

// Enrich fills missing context and per-million prices from the catalog.
func Enrich(info Info) Info {
	fb, ok := catalog[info.ID]
	if !ok {
		return info
	}
	if info.Context <= 0 {
		info.Context = fb.Context
	}
	if info.InputPerM <= 0 {
		info.InputPerM = fb.InputPerM
	}
	if info.OutputPerM <= 0 {
		info.OutputPerM = fb.OutputPerM
	}
	return info
}

// Lookup returns the catalog row for id, or a zero Info.
func Lookup(id string) Info {
	return catalog[id]
}
