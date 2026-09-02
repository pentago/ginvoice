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
	fontExtractOnce sync.Once
	fontExtractErr  error

	fontConfMu  sync.RWMutex
	fontConf    text.FontConfiguration
	fontConfErr error
)

// fontConfig lazily extracts the embedded fonts and builds the font
// configuration. Extraction happens once per process; the configuration is
// rebuilt (and swapped) whenever reloadFonts is called, e.g. after new
// Google Fonts are downloaded.
func fontConfig() (text.FontConfiguration, error) {
	fontConfMu.RLock()
	if fontConf != nil {
		fc := fontConf
		err := fontConfErr
		fontConfMu.RUnlock()
		return fc, err
	}
	fontConfMu.RUnlock()

	fontConfMu.Lock()
	defer fontConfMu.Unlock()
	if fontConf != nil {
		return fontConf, fontConfErr
	}
	fontConfErr = buildFontConfig()
	return fontConf, fontConfErr
}

// reloadFonts rescans the fonts directory and swaps the active font
// configuration under the write lock. Extraction is not re-run; it is
// idempotent and keyed to the first dataDir seen.
func reloadFonts() {
	fontConfMu.Lock()
	defer fontConfMu.Unlock()
	fontConfErr = buildFontConfig()
}

func buildFontConfig() error {
	fontExtractOnce.Do(func() {
		fontExtractErr = extractFonts()
	})
	if fontExtractErr != nil {
		return fontExtractErr
	}

	fontExtractDir := filepath.Join(dataDir(), "fonts")
	cfg := fontconfig.Standard.Copy()
	fs, err := cfg.ScanFontDirectories(fontExtractDir)
	if err != nil {
		return fmt.Errorf("scan fonts: %w", err)
	}
	fontConf = text.NewFontConfigurationPango(fcfonts.NewFontMap(cfg, fs))
	return nil
}

func extractFonts() error {
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
	return nil
}
