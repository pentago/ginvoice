package pdf

import "encoding/json"

// TemplateConfig controls the visual style of the PDF invoice. Every field
// maps directly to a maroto rendering decision. Missing keys in a partial JSON
// config fall back to the DefaultConfig values via LoadConfig.
type TemplateConfig struct {
	AccentColor      string  `json:"accent_color"`
	TextColor        string  `json:"text_color"`
	MutedColor       string  `json:"muted_color"`
	DividerColor     string  `json:"divider_color"`
	TableHeaderBg    string  `json:"table_header_bg"`
	TableHeaderColor string  `json:"table_header_color"`
	HeadingSize      float64 `json:"heading_size"`
	BodySize         float64 `json:"body_size"`
	LabelSize        float64 `json:"label_size"`
	MarginMM         float64 `json:"margin_mm"`
	ShowNotes        bool    `json:"show_notes"`
}

// DefaultConfig returns the Canva minimal template defaults: monochrome
// charcoal on near-white, generous margins, Helvetica.
func DefaultConfig() TemplateConfig {
	return TemplateConfig{
		AccentColor:      "#1F1F1F",
		TextColor:        "#333333",
		MutedColor:       "#6A6A6A",
		DividerColor:     "#D7D7D7",
		TableHeaderBg:    "",
		TableHeaderColor: "#333333",
		HeadingSize:      36,
		BodySize:         10,
		LabelSize:        8,
		MarginMM:         12,
		ShowNotes:        true,
	}
}

// LoadConfig parses a JSON config string, starting from DefaultConfig and
// overlaying only the keys present in the JSON. An empty or invalid JSON
// string yields the defaults — rendering never fails due to config.
func LoadConfig(jsonStr string) TemplateConfig {
	cfg := DefaultConfig()
	if jsonStr == "" {
		return cfg
	}
	_ = json.Unmarshal([]byte(jsonStr), &cfg)
	return cfg
}

// JSON returns the config serialized as indented JSON, suitable for display
// in a textarea editor.
func (c TemplateConfig) JSON() (string, error) {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
