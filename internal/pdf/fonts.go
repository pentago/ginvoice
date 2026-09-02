package pdf

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/benoitkugler/textprocessing/fontconfig"
	"github.com/benoitkugler/textprocessing/pango/fcfonts"
	"github.com/benoitkugler/webrender/text"
)

// fontFiles holds the DejaVu Sans TTFs rendered into every invoice.
// Static TrueType only: go-weasyprint silently drops text for variable
// fonts and TTC collections.
//
//go:embed fonts/*.ttf
var fontFiles embed.FS

// dataDir is the app's guaranteed-writable directory (the SQLite DB lives
// there). Overridable via GINVOICE_DATA_DIR so tests and dev machines
// without /data work.
func dataDir() string {
	if d := os.Getenv("GINVOICE_DATA_DIR"); d != "" {
		return d
	}
	return "/data"
}

var (
	fontConfOnce sync.Once
	fontConf     text.FontConfiguration
	fontConfErr  error
)

// fontConfig lazily extracts the embedded fonts and builds the font
// configuration, once per process.
func fontConfig() (text.FontConfiguration, error) {
	fontConfOnce.Do(func() {
		fontConfErr = setupFonts()
	})
	return fontConf, fontConfErr
}

func setupFonts() error {
	fontExtractDir := filepath.Join(dataDir(), "fonts")
	if err := os.MkdirAll(fontExtractDir, 0o755); err != nil {
		return fmt.Errorf("create font dir: %w", err)
	}
	entries, err := fontFiles.ReadDir("fonts")
	if err != nil {
		return err
	}
	for _, e := range entries {
		b, err := fontFiles.ReadFile("fonts/" + e.Name())
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(fontExtractDir, e.Name()), b, 0o644); err != nil {
			return fmt.Errorf("extract font %s: %w", e.Name(), err)
		}
	}

	cfg := fontconfig.Standard.Copy()
	fs, err := cfg.ScanFontDirectories(fontExtractDir)
	if err != nil {
		return fmt.Errorf("scan fonts: %w", err)
	}
	fontConf = text.NewFontConfigurationPango(fcfonts.NewFontMap(cfg, fs))
	return nil
}
