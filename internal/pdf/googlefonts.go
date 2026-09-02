package pdf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	catalogCacheFile = "fonts-catalog.json"
	catalogMaxAge    = 7 * 24 * time.Hour
)

// css2Base returns the base URL for the css2 font API. Overridable via
// GINVOICE_GOOGLE_FONTS_BASE_URL (read per call) for tests.
func css2Base() string {
	if b := os.Getenv("GINVOICE_GOOGLE_FONTS_BASE_URL"); b != "" {
		return b
	}
	return "https://fonts.googleapis.com"
}

// metadataBase returns the base URL for the font metadata catalog.
// Overridable via GINVOICE_GOOGLE_FONTS_BASE_URL (read per call) for tests.
func metadataBase() string {
	if b := os.Getenv("GINVOICE_GOOGLE_FONTS_BASE_URL"); b != "" {
		return b
	}
	return "https://fonts.google.com"
}

// FontCatalog returns the sorted list of available Google Font family names,
// cached at dataDir()/fonts-catalog.json. The cache is refetched only when
// missing or older than 7 days. On any failure it returns nil (never an
// error) and keeps serving any existing cache.
func FontCatalog() []string {
	cachePath := filepath.Join(dataDir(), catalogCacheFile)
	if names, ok := readCatalogCache(cachePath); ok {
		return names
	}
	names, err := fetchCatalog()
	if err != nil {
		if stale, ok := readCatalogCacheAny(cachePath); ok {
			return stale
		}
		return nil
	}
	writeCatalogCache(cachePath, names)
	return names
}

func readCatalogCache(path string) ([]string, bool) {
	info, err := os.Stat(path)
	if err != nil || time.Since(info.ModTime()) > catalogMaxAge {
		return nil, false
	}
	return readCatalogCacheAny(path)
}

func readCatalogCacheAny(path string) ([]string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var names []string
	if err := json.Unmarshal(b, &names); err != nil {
		return nil, false
	}
	return names, true
}

func writeCatalogCache(path string, names []string) {
	b, err := json.Marshal(names)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, b, 0o644)
}

func fetchCatalog() ([]string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(metadataBase() + "/metadata/fonts")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	// The catalog may carry an XSSI prefix (e.g. ")]}'"); strip everything
	// before the first '{'.
	idx := bytes.IndexByte(body, '{')
	if idx < 0 {
		return nil, fmt.Errorf("no JSON object in catalog response")
	}
	var catalog struct {
		FamilyMetadataList []struct {
			Family string `json:"family"`
		} `json:"familyMetadataList"`
	}
	if err := json.Unmarshal(body[idx:], &catalog); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(catalog.FamilyMetadataList))
	for _, f := range catalog.FamilyMetadataList {
		if f.Family != "" {
			names = append(names, f.Family)
		}
	}
	sort.Strings(names)
	return names, nil
}

// DownloadFamilyFonts fetches the static TTFs for a font family and writes
// them as <slug>-<weight>.ttf into dataDir()/fonts. It is a no-op when the
// 400-weight file already exists. On success it reloads the font
// configuration so the new fonts become available to the renderer.
func DownloadFamilyFonts(family string) error {
	slug := slugify(family)
	fontDir := filepath.Join(dataDir(), "fonts")
	if _, err := os.Stat(filepath.Join(fontDir, slug+"-400.ttf")); err == nil {
		return nil // already cached
	}

	css, err := fetchCSS(family)
	if err != nil {
		return err
	}
	weights := parseFontFaces(css)
	if len(weights) == 0 {
		return fmt.Errorf("no font faces found for family %q", family)
	}
	if err := os.MkdirAll(fontDir, 0o755); err != nil {
		return err
	}
	for weight, src := range weights {
		ttf, err := downloadTTF(src)
		if err != nil {
			return fmt.Errorf("download %s-%s: %w", slug, weight, err)
		}
		if err := os.WriteFile(filepath.Join(fontDir, slug+"-"+weight+".ttf"), ttf, 0o644); err != nil {
			return err
		}
	}
	reloadFonts()
	return nil
}

func fetchCSS(family string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	u := css2Base() + "/css2?family=" + url.QueryEscape(family) + ":ital,wght@0,400;0,700&display=swap"
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return "", err
	}
	// A non-browser UA is required: browser UAs return woff2/variable fonts,
	// MSIE UAs return EOT — both useless here.
	req.Header.Set("User-Agent", "curl/8.0.0")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusBadRequest {
		return "", fmt.Errorf("unknown font family %q", family)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("css2 returned status %d for family %q", resp.StatusCode, family)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

var (
	fontWeightRe = regexp.MustCompile(`(?i)font-weight\s*:\s*([^;]+);`)
	srcURLRe     = regexp.MustCompile(`(?i)src\s*:\s*url\(([^)]+)\)`)
)

// parseFontFaces extracts a map of font-weight → TTF src URL from the css2
// response, one entry per @font-face block.
func parseFontFaces(css string) map[string]string {
	faces := map[string]string{}
	for _, block := range fontFaceBlocks(css) {
		wm := fontWeightRe.FindStringSubmatch(block)
		m := srcURLRe.FindStringSubmatch(block)
		if wm == nil || m == nil {
			continue
		}
		weight := strings.TrimSpace(wm[1])
		if weight == "" {
			continue
		}
		faces[weight] = strings.Trim(strings.TrimSpace(m[1]), `"'`)
	}
	return faces
}

func fontFaceBlocks(css string) []string {
	var blocks []string
	for {
		i := strings.Index(css, "@font-face")
		if i < 0 {
			break
		}
		open := strings.IndexByte(css[i:], '{')
		if open < 0 {
			break
		}
		open += i
		close := strings.IndexByte(css[open:], '}')
		if close < 0 {
			break
		}
		close += open
		blocks = append(blocks, css[open+1:close])
		css = css[close+1:]
	}
	return blocks
}

func downloadTTF(src string) ([]byte, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(src)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ttf returned status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// slugify lowercases a family name, converts spaces to dashes, and strips
// every other character.
func slugify(family string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(family) {
		switch {
		case r == ' ':
			b.WriteByte('-')
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		}
	}
	return b.String()
}
