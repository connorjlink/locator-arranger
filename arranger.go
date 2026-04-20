package main

import (
    "encoding/json"
    "flag"
    "fmt"
    "html/template"
    "image"
    _ "image/gif"
    _ "image/jpeg"
    _ "image/png"
    "io/fs"
    "math"
    "os"
    "path/filepath"
    "sort"
    "strconv"
    "strings"
    "time"
)

type Theme struct {
	OceanColor  string `json:"oceancolor"`
	LandColor   string `json:"landcolor"`
	WaterColor  string `json:"watercolor"`
	BorderColor string `json:"bordercolor"`
	StarColor   string `json:"starcolor"`
	InsetColor  string `json:"insetcolor"`
	TextColor   string `json:"textcolor"`
	FontFamily  string `json:"fontfamily"`
}

type Record struct {
    Filename   string
    Caption    string
    Location   string
    DateRaw    string
    Date       time.Time
    TakenAtRaw string
    TakenAt    time.Time
    Lat        float64
    Lon        float64

    PhotoAbs    string
    MapAbs      string
    AspectRatio float64
    Orientation string
}

type EntryView struct {
    Filename    string
    Caption     string
    Location    string
    Date        string
    TakenAt     string
    PhotoSrc    string
    MapSrc      string
    Lat         float64
    Lon         float64
    LatPretty   string
    LonPretty   string
    AspectRatio float64
    Orientation string
    TakenSort   int64
}

type DayGroup struct {
	Label   string
	SortKey time.Time
	Entries []EntryView
}

type SummaryImage struct {
	Name string
	Src  string
}

type PageData struct {
    Theme      Theme
    Generated  string
    Summaries  []SummaryImage
    DayGroups  []DayGroup
    TotalItems int
    PackMode   string
}

func main() {
	var (
        pointsArg    string
        summariesDir string
        photosDir    string
        mapsDir      string
        themePath    string
        outPath      string
        title        string
        packMode     string
    )

	flag.StringVar(&pointsArg, "points", "", "Comma-separated .points.csv files or glob patterns (optional if -summaries provided)")
	flag.StringVar(&summariesDir, "summaries", "", "Summaries directory (contains *.points.csv and summary pngs)")
	flag.StringVar(&photosDir, "photos", "", "Original photos root directory")
	flag.StringVar(&mapsDir, "maps", "", "Locator maps directory")
	flag.StringVar(&themePath, "theme", "", "Theme JSON path")
	flag.StringVar(&outPath, "out", "album.html", "Output HTML file path")
	flag.StringVar(&title, "title", "Photo Album", "Album title")
	flag.StringVar(&packMode, "pack", "chronological", "Album packing mode: chronological|smart")

	flag.Parse()

	if photosDir == "" || mapsDir == "" || themePath == "" {
		fail("required: -photos, -maps, -theme (and either -points or -summaries)")
	}
	if pointsArg == "" && summariesDir == "" {
		fail("provide either -points or -summaries")
	}

	packMode = strings.ToLower(strings.TrimSpace(packMode))
    if packMode != "chronological" && packMode != "smart" && packMode != "masonry" {
        fail("invalid -pack value. use chronological, smart, or masonry")
    }

	theme := loadTheme(themePath)
	applyThemeDefaults(&theme)

	pointsFiles := collectPointsFiles(pointsArg, summariesDir)
	if len(pointsFiles) == 0 {
		fail("no .points.csv files found")
	}

	photoIndex := buildPhotoIndex(photosDir)
	mapIndex := buildMapIndex(mapsDir)

	aspectCache := map[string]float64{}

    records := []Record{}
    seen := map[string]struct{}{}
    for _, p := range pointsFiles {
        rs := parsePointsCSV(p)
        for _, r := range rs {
            key := r.Filename + "|" + r.DateRaw + "|" + fmt.Sprintf("%.6f,%.6f", r.Lat, r.Lon)
            if _, ok := seen[key]; ok {
                continue
            }
            seen[key] = struct{}{}

            if abs, ok := photoIndex[strings.ToLower(r.Filename)]; ok {
                r.PhotoAbs = abs
                if ar, ok := aspectCache[abs]; ok {
                    r.AspectRatio = ar
                } else {
                    r.AspectRatio = detectAspectRatio(abs)
                    aspectCache[abs] = r.AspectRatio
                }
                r.Orientation = classifyOrientation(r.AspectRatio)
            } else {
                r.Orientation = "unknown"
            }

            stem := strings.TrimSuffix(strings.ToLower(r.Filename), strings.ToLower(filepath.Ext(r.Filename)))
            if abs, ok := mapIndex[stem]; ok {
                r.MapAbs = abs
            }
            records = append(records, r)
        }
    }

	dayGroups := groupByDate(records, outPath, packMode)
	summaries := collectSummaryImages(summariesDir, outPath)

	page := PageData{
        Theme:      theme,
        Generated:  time.Now().Format("2006-01-02 15:04"),
        Summaries:  summaries,
        DayGroups:  dayGroups,
        TotalItems: countEntries(dayGroups),
        PackMode:   packMode,
    }

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil && filepath.Dir(outPath) != "." {
		fail(err.Error())
	}
	f, err := os.Create(outPath)
	if err != nil {
		fail(err.Error())
	}
	defer f.Close()

	tpl := template.Must(template.New("album").Parse(htmlTemplate))
	if err := tpl.Execute(f, map[string]any{
		"Title": title,
		"Page":  page,
	}); err != nil {
		fail(err.Error())
	}

	fmt.Printf("Wrote album: %s\n", outPath)
}

func loadTheme(path string) Theme {
	b, err := os.ReadFile(path)
	if err != nil {
		fail("theme read error: " + err.Error())
	}
	var t Theme
	if err := json.Unmarshal(b, &t); err != nil {
		fail("theme parse error: " + err.Error())
	}
	return t
}

func applyThemeDefaults(t *Theme) {
	if t.OceanColor == "" {
		t.OceanColor = "#000000"
	}
	if t.LandColor == "" {
		t.LandColor = "#0F0F0F"
	}
	if t.BorderColor == "" {
		t.BorderColor = "#A4A4A4"
	}
	if t.StarColor == "" {
		t.StarColor = "#C8A51C"
	}
	if t.InsetColor == "" {
		t.InsetColor = "#880000"
	}
	if t.TextColor == "" {
		t.TextColor = "#FFFFFF"
	}
	if t.FontFamily == "" {
		t.FontFamily = "serif"
	}
	if t.WaterColor == "" {
		t.WaterColor = "#29449b"
	}
}

func collectPointsFiles(pointsArg, summariesDir string) []string {
	var out []string
	seen := map[string]struct{}{}

	add := func(p string) {
		ap, err := filepath.Abs(p)
		if err != nil {
			return
		}
		if _, ok := seen[ap]; ok {
			return
		}
		if strings.HasSuffix(strings.ToLower(ap), ".points.csv") {
			if st, err := os.Stat(ap); err == nil && !st.IsDir() {
				seen[ap] = struct{}{}
				out = append(out, ap)
			}
		}
	}

	if pointsArg != "" {
		for _, token := range strings.Split(pointsArg, ",") {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			matches, err := filepath.Glob(token)
			if err == nil && len(matches) > 0 {
				for _, m := range matches {
					add(m)
				}
				continue
			}
			add(token)
		}
	}

	if summariesDir != "" {
		_ = filepath.WalkDir(summariesDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if strings.HasSuffix(strings.ToLower(path), ".points.csv") {
				add(path)
			}
			return nil
		})
	}

	sort.Strings(out)
	return out
}

func buildPhotoIndex(root string) map[string]string {
	index := map[string]string{}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		extension := strings.ToLower(filepath.Ext(path))
		switch extension {
		case ".jpg", ".jpeg", ".png", ".heic", ".heif":
			name := strings.ToLower(filepath.Base(path))
			index[name] = path
		}
		return nil
	})
	return index
}

func buildMapIndex(root string) map[string]string {
	index := map[string]string{}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		extension := strings.ToLower(filepath.Ext(path))
		switch extension {
		case ".png", ".jpg", ".jpeg":
			base := strings.ToLower(filepath.Base(path))
			stem := strings.TrimSuffix(base, extension)
			index[stem] = path
		}
		return nil
	})
	return index
}

func parsePointsCSV(path string) []Record {
	lines, err := readLines(path)
	if err != nil {
		return nil
	}

	var out []Record
	var pending *Record

	for _, raw := range lines {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}

		if strings.HasPrefix(s, "#") {
			if r, ok := parseMetaComment(strings.TrimSpace(strings.TrimPrefix(s, "#"))); ok {
				tmp := r
				pending = &tmp
			}
			continue
		}

		lat, lon, ok := parseCoordLine(s)
		if !ok {
			continue
		}
		if pending == nil {
			continue
		}
		pending.Lat = lat
		pending.Lon = lon
		out = append(out, *pending)
		pending = nil
	}

	return out
}

func parseMetaComment(s string) (Record, bool) {
    parts := strings.Split(s, " | ")
    if len(parts) < 2 {
        return Record{}, false
    }

    filename := strings.TrimSpace(parts[0])
    if filename == "" {
        return Record{}, false
    }

    kv := map[string]string{}
    for _, p := range parts[1:] {
        x := strings.SplitN(p, "=", 2)
        if len(x) != 2 {
            continue
        }
        k := strings.ToLower(strings.TrimSpace(x[0]))
        v := strings.TrimSpace(x[1])
        kv[k] = v
    }

    r := Record{
        Filename:   filename,
        Caption:    kv["caption"],
        Location:   kv["location"],
        DateRaw:    kv["date"],
        TakenAtRaw: kv["taken_at"],
    }

    if d, err := time.Parse("Monday, January 2, 2006", r.DateRaw); err == nil {
        r.Date = d
    }
    if t, ok := parseTakenAt(r.TakenAtRaw); ok {
        r.TakenAt = t
        if r.Date.IsZero() {
            r.Date = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
        }
        if strings.TrimSpace(r.DateRaw) == "" {
            r.DateRaw = t.Format("Monday, January 2, 2006")
        }
    }
    return r, true
}

func parseTakenAt(s string) (time.Time, bool) {
    s = strings.TrimSpace(s)
    if s == "" || strings.EqualFold(s, "NULL") {
        return time.Time{}, false
    }
    layouts := []string{
        "2006-01-02T15:04:05",
        time.RFC3339,
    }
    for _, layout := range layouts {
        if t, err := time.Parse(layout, s); err == nil {
            return t, true
        }
    }
    return time.Time{}, false
}

func parseCoordLine(s string) (float64, float64, bool) {
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return 0, 0, false
	}
	lat, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lon, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return lat, lon, true
}

func detectAspectRatio(path string) float64 {
    f, err := os.Open(path)
    if err != nil {
        return 0
    }
    defer f.Close()
    cfg, _, err := image.DecodeConfig(f)
    if err != nil || cfg.Height <= 0 {
        return 0
    }
    return float64(cfg.Width) / float64(cfg.Height)
}

func classifyOrientation(ar float64) string {
    if ar <= 0 {
        return "unknown"
    }
    if ar > 1.10 {
        return "landscape"
    }
    if ar < 0.90 {
        return "portrait"
    }
    return "square"
}

func orientationRank(o string) int {
    switch o {
    case "landscape":
        return 0
    case "square":
        return 1
    case "portrait":
        return 2
    default:
        return 3
    }
}

func groupByDate(records []Record, outHTML string, packMode string) []DayGroup {
    m := map[string]*DayGroup{}

    for _, r := range records {
        label := strings.TrimSpace(r.DateRaw)
        sortKey := r.Date
        if label == "" && !r.TakenAt.IsZero() {
            label = r.TakenAt.Format("Monday, January 2, 2006")
        }
        if label == "" {
            label = "Unknown Date"
        }
        if sortKey.IsZero() && !r.TakenAt.IsZero() {
            sortKey = time.Date(r.TakenAt.Year(), r.TakenAt.Month(), r.TakenAt.Day(), 0, 0, 0, 0, r.TakenAt.Location())
        }
        if sortKey.IsZero() {
            sortKey = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)
        }

        g, ok := m[label]
        if !ok {
            g = &DayGroup{Label: label, SortKey: sortKey}
            m[label] = g
        }

        takenLabel := "Unknown"
        takenSort := int64(1<<62 - 1)
        if !r.TakenAt.IsZero() {
            takenLabel = r.TakenAt.Format("15:04:05")
            takenSort = r.TakenAt.Unix()
        }

        g.Entries = append(g.Entries, EntryView{
            Filename:    r.Filename,
            Caption:     nilToUnknown(r.Caption),
            Location:    nilToUnknown(r.Location),
            Date:        label,
            TakenAt:     takenLabel,
            PhotoSrc:    makeSrc(outHTML, r.PhotoAbs),
            MapSrc:      makeSrc(outHTML, r.MapAbs),
            Lat:         r.Lat,
            Lon:         r.Lon,
            LatPretty:   formatLatDMS(r.Lat),
            LonPretty:   formatLonDMS(r.Lon),
            AspectRatio: r.AspectRatio,
            Orientation: r.Orientation,
            TakenSort:   takenSort,
        })
    }

    var groups []DayGroup
    for _, g := range m {
        switch packMode {
		// group orientation, then by sort by time
        case "smart":
            sort.SliceStable(g.Entries, func(i, j int) bool {
                ri, rj := orientationRank(g.Entries[i].Orientation), orientationRank(g.Entries[j].Orientation)
                if ri != rj {
                    return ri < rj
                }
                if g.Entries[i].TakenSort != g.Entries[j].TakenSort {
                    return g.Entries[i].TakenSort < g.Entries[j].TakenSort
                }
                return strings.ToLower(g.Entries[i].Filename) < strings.ToLower(g.Entries[j].Filename)
            })
		// keep chronological flow, but prefer larger aspect first on ties for better visual fill
        case "masonry":
            sort.SliceStable(g.Entries, func(i, j int) bool {
                if g.Entries[i].TakenSort != g.Entries[j].TakenSort {
                    return g.Entries[i].TakenSort < g.Entries[j].TakenSort
                }
                if g.Entries[i].AspectRatio != g.Entries[j].AspectRatio {
                    return g.Entries[i].AspectRatio > g.Entries[j].AspectRatio
                }
                return strings.ToLower(g.Entries[i].Filename) < strings.ToLower(g.Entries[j].Filename)
            })
		// chronological
        default:
            sort.SliceStable(g.Entries, func(i, j int) bool {
                if g.Entries[i].TakenSort != g.Entries[j].TakenSort {
                    return g.Entries[i].TakenSort < g.Entries[j].TakenSort
                }
                return strings.ToLower(g.Entries[i].Filename) < strings.ToLower(g.Entries[j].Filename)
            })
        }
        groups = append(groups, *g)
    }

    sort.Slice(groups, func(i, j int) bool {
        return groups[i].SortKey.After(groups[j].SortKey)
    })
    return groups
}

func formatDMS(value float64, positiveDir, negativeDir string) string {
    dir := positiveDir
    if value < 0 {
        dir = negativeDir
    }

    v := math.Abs(value)
    deg := int(v)
    minutesFloat := (v - float64(deg)) * 60.0
    min := int(minutesFloat)
    sec := (minutesFloat - float64(min)) * 60.0

    sec = math.Round(sec*100) / 100
    if sec >= 60.0 {
        sec = 0
        min++
    }
    if min >= 60 {
        min = 0
        deg++
    }

    return fmt.Sprintf("%d°%d′%.0f″ %s", deg, min, sec, dir)
}

func formatLatDMS(lat float64) string {
    return formatDMS(lat, "N", "S")
}

func formatLonDMS(lon float64) string {
    return formatDMS(lon, "E", "W")
}

func collectSummaryImages(summariesDir, outHTML string) []SummaryImage {
	if summariesDir == "" {
		return nil
	}
	var imgs []SummaryImage
	_ = filepath.WalkDir(summariesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		l := strings.ToLower(path)
		if strings.HasSuffix(l, ".png") && !strings.HasSuffix(l, ".points.csv.png") {
			imgs = append(imgs, SummaryImage{
				Name: filepath.Base(path),
				Src:  makeSrc(outHTML, path),
			})
		}
		return nil
	})
	sort.Slice(imgs, func(i, j int) bool { return imgs[i].Name < imgs[j].Name })
	return imgs
}

func makeSrc(outHTMLPath, targetAbs string) string {
	if targetAbs == "" {
		return ""
	}
	outDir := filepath.Dir(outHTMLPath)
	rel, err := filepath.Rel(outDir, targetAbs)
	if err != nil {
		rel = targetAbs
	}
	return filepath.ToSlash(rel)
}

func readLines(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Split(s, "\n"), nil
}

func nilToUnknown(s string) string {
	if strings.TrimSpace(s) == "" || strings.EqualFold(s, "NULL") {
		return "Unknown"
	}
	return s
}

func countEntries(groups []DayGroup) int {
	n := 0
	for _, g := range groups {
		n += len(g.Entries)
	}
	return n
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "error:", msg)
	os.Exit(1)
}

const htmlTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width,initial-scale=1" />
<title>{{.Title}}</title>
<style>
:root{
  --bg: {{.Page.Theme.OceanColor}};
  --card: {{.Page.Theme.LandColor}};
  --text: {{.Page.Theme.TextColor}};
  --muted: {{.Page.Theme.BorderColor}};
  --accent: {{.Page.Theme.StarColor}};
  --accent2: {{.Page.Theme.InsetColor}};
  --water: {{.Page.Theme.WaterColor}};
  --font: {{.Page.Theme.FontFamily}};
}
*{box-sizing:border-box}
body{
  margin:0;padding:24px;background:var(--bg);color:var(--text);
  font-family:var(--font), system-ui, -apple-system, Segoe UI, Roboto, sans-serif;
}
header{
  border:1px solid var(--muted); background:var(--card); border-radius:12px;
  padding:16px 18px; margin-bottom:18px;
}
h1{margin:0 0 8px;font-size:1.5rem}
.sub{color:var(--muted);font-size:.9rem}
.summary-grid{
  display:grid;grid-template-columns:repeat(auto-fit,minmax(240px,1fr));gap:10px;margin-top:12px;
}
.summary-grid img{
  width:100%;height:auto;border-radius:10px;border:1px solid var(--muted);display:block;
}
.day{
  margin:18px 0 24px; border-left:4px solid var(--accent); padding-left:10px;
}
.day h2{
  margin:0 0 10px; font-size:1.05rem; color:var(--accent);
}
.entries{
  display:grid; grid-template-columns:repeat(auto-fit,minmax(340px,1fr)); gap:12px;
}
.entries.smart{
  display:flex; flex-wrap:wrap; gap:12px;
}
.entries.smart .entry{ flex: 1 1 360px; }
.entries.smart .entry.landscape{ flex-basis: 460px; }
.entries.smart .entry.portrait{ flex-basis: 300px; }

.entries.masonry{
  column-width: 380px;
  column-gap: 12px;
  display: block;
}
.entries.masonry .entry{
  display: inline-block;
  width: 100%;
  margin: 0 0 12px;
  break-inside: avoid;
  -webkit-column-break-inside: avoid;
}

.entry{
  background:var(--card); border:1px solid var(--muted); border-radius:12px; padding:10px;
}
.entry-head{
  margin-bottom:8px; padding-bottom:6px; border-bottom:1px dashed var(--muted);
}
.cap{font-weight:700}
.meta{font-size:.88rem;color:var(--muted)}

.photo-wrap{ position:relative; }
.photo-wrap img{
  width:100%; height:auto; display:block; border-radius:8px; border:1px solid var(--muted); background:#000;
}
.photo-hint{
  margin-top:6px;
  font-size:.78rem;
  color:var(--muted);
}

.missing{
  border:1px dashed var(--accent2); border-radius:8px; padding:12px; font-size:.85rem; color:var(--muted);
}
small.coords{display:block;margin-top:8px;color:var(--muted)}

.map-flyover{
  position:fixed;
  z-index:9999;
  width:min(420px, 42vw);
  max-width:calc(100vw - 16px);
  background:var(--card);
  border:1px solid var(--muted);
  border-radius:10px;
  box-shadow:0 12px 36px rgba(0,0,0,.45);
  padding:8px;
  opacity:0;
  transform:scale(.98);
  transition:opacity .12s ease, transform .12s ease;
  pointer-events:auto;
  visibility:hidden;
}
.map-flyover.visible{
  opacity:1;
  transform:scale(1);
  visibility:visible;
}
.map-flyover img{
  width:100%;
  height:auto;
  display:block;
  border-radius:6px;
  border:1px solid var(--muted);
  background:#000;
}
.map-flyover .fly-meta{
  margin-top:6px;
  color:var(--muted);
  font-size:.78rem;
}
.map-flyover{
  position:fixed;
  z-index:9999;
  width:min(420px, 42vw);
  max-width:calc(100vw - 16px);
  background:var(--card);
  border:1px solid var(--muted);
  border-radius:10px;
  box-shadow:0 12px 36px rgba(0,0,0,.45);
  padding:8px;
  opacity:0;
  transform:scale(.98);
  transition:opacity .12s ease, transform .12s ease;
  pointer-events:auto;
  visibility:hidden;
}
.map-flyover.visible{
  opacity:1;
  transform:scale(1);
  visibility:visible;
}
.map-flyover img{
  width:100%;
  height:auto;
  display:block;
  border-radius:6px;
  border:1px solid var(--muted);
  background:#000;
  cursor:zoom-in;
}
.map-flyover .fly-meta{
  margin-top:6px;
  color:var(--muted);
  font-size:.78rem;
}
  .map-flyover .fly-head{
  margin-bottom:6px;
  padding-bottom:6px;
  border-bottom:1px dashed var(--muted);
}
.map-flyover .fly-cap{
  font-weight:700;
  line-height:1.2;
}
.map-flyover .fly-sub{
  margin-top:2px;
  color:var(--muted);
  font-size:.78rem;
}
.map-modal{
  position:fixed;
  inset:0;
  z-index:10000;
  display:none;
}
.map-modal.open{ display:block; }
.map-modal-backdrop{
  position:absolute;
  inset:0;
  background:rgba(0,0,0,.72);
}
.map-modal-dialog{
  position:absolute;
  inset:24px;
  display:flex;
  align-items:center;
  justify-content:center;
  pointer-events:none;
}
.map-modal-content{
  position:relative;
  max-width:min(1400px, calc(100vw - 48px));
  max-height:calc(100vh - 48px);
  background:var(--card);
  border:1px solid var(--muted);
  border-radius:12px;
  padding:10px;
  box-shadow:0 24px 54px rgba(0,0,0,.55);
  pointer-events:auto;

  display:flex;
  flex-direction:column;
  overflow:hidden;
}

.map-modal-content img{
  display:block;
  max-width:100%;
  max-height:calc(100vh - 160px);
  width:auto;
  height:auto;
  border:1px solid var(--muted);
  border-radius:8px;
  background:#000;
  object-fit:contain;
  flex:1 1 auto;
  min-height:0;
}
.map-modal-head{
  margin: 0 42px 10px 0;
  padding-bottom: 8px;
  border-bottom: 1px dashed var(--muted);
}
.map-modal-cap{
  font-weight:700;
  line-height:1.2;
}
.map-modal-sub{
  margin-top:2px;
  color:var(--muted);
  font-size:.82rem;
}
.map-modal-close{
  position:absolute;
  top:8px;
  right:8px;
  width:32px;
  height:32px;
  border-radius:999px;
  border:1px solid var(--muted);
  background:var(--bg);
  color:var(--text);
  font-size:20px;
  line-height:1;
  cursor:pointer;
}
</style>
</head>
<body>
  <header>
    <h1>{{.Title}}</h1>
    <div class="sub">Entries: {{.Page.TotalItems}} · Generated: {{.Page.Generated}} · Pack: {{.Page.PackMode}}</div>
    {{if .Page.Summaries}}
    <div class="summary-grid">
      {{range .Page.Summaries}}
      <figure style="margin:0">
        <img src="{{.Src}}" alt="{{.Name}}">
        <figcaption class="sub" style="margin-top:4px">{{.Name}}</figcaption>
      </figure>
      {{end}}
    </div>
    {{end}}
  </header>

  {{range .Page.DayGroups}}
  <section class="day">
    <h2>{{.Label}}</h2>
    <div class="entries {{$.Page.PackMode}}">
      {{range .Entries}}
      <article class="entry {{.Orientation}}">
        <div class="entry-head">
          <div class="cap">{{.Caption}}</div>
          <div class="meta">{{.Filename}} · {{.Location}} · {{.TakenAt}} · {{.Orientation}}</div>
        </div>

        <div class="photo-wrap">
          {{if .PhotoSrc}}
            <img
              class="photo-trigger"
              src="{{.PhotoSrc}}"
              alt="{{.Filename}}"
              data-map-src="{{.MapSrc}}"
              data-map-alt="Locator map for {{.Filename}}"
              data-caption="{{.Caption}}"
              data-filename="{{.Filename}}"
              data-location="{{.Location}}"
              data-taken="{{.TakenAt}}"
              data-orientation="{{.Orientation}}"
            >
            {{if .MapSrc}}
              <div class="photo-hint">Hover photo to preview locator map</div>
            {{else}}
              <div class="photo-hint">Locator map missing</div>
            {{end}}
          {{else}}
            <div class="missing">Original photo missing</div>
          {{end}}
        </div>

        <small class="coords">{{.LatPretty}}, {{.LonPretty}}</small>
      </article>
      {{end}}
    </div>
  </section>
  {{end}}

  <div id="mapFlyover" class="map-flyover" aria-hidden="true">
    <div class="fly-head">
      <div id="mapFlyoverCap" class="fly-cap"></div>
      <div id="mapFlyoverSub" class="fly-sub"></div>
    </div>
    <img id="mapFlyoverImg" src="" alt="Locator map preview">
    <div id="mapFlyoverMeta" class="fly-meta">Locator preview · click to magnify</div>
  </div>

  <div id="mapModal" class="map-modal" aria-hidden="true">
    <div id="mapModalBackdrop" class="map-modal-backdrop"></div>
    <div class="map-modal-dialog">
      <div class="map-modal-content" role="dialog" aria-modal="true" aria-label="Locator map">
        <button id="mapModalClose" class="map-modal-close" type="button" aria-label="Close">×</button>
        <div class="map-modal-head">
          <div id="mapModalCap" class="map-modal-cap"></div>
          <div id="mapModalSub" class="map-modal-sub"></div>
        </div>
        <img id="mapModalImg" src="" alt="Locator map large preview">
      </div>
    </div>
  </div>
<script>
(() => {
  const fly = document.getElementById("mapFlyover");
  const flyImg = document.getElementById("mapFlyoverImg");
  const flyMeta = document.getElementById("mapFlyoverMeta");
  const flyCap = document.getElementById("mapFlyoverCap");
  const flySub = document.getElementById("mapFlyoverSub");
  const triggers = Array.from(document.querySelectorAll(".photo-trigger"));

  const modal = document.getElementById("mapModal");
  const modalImg = document.getElementById("mapModalImg");
  const modalClose = document.getElementById("mapModalClose");
  const modalBackdrop = document.getElementById("mapModalBackdrop");
  const modalCap = document.getElementById("mapModalCap");
  const modalSub = document.getElementById("mapModalSub");

  if (!fly || !flyImg || !flyMeta || !flyCap || !flySub) return;

  const hasModal = !!(modal && modalImg && modalClose && modalBackdrop && modalCap && modalSub);

  const MARGIN = 8;
  const EDGE_OVERLAP = 26;
  const POINTER_PAD = 10;

  let hideTimer = null;
  let activeTrigger = null;
  let anchorX = 0, anchorY = 0;

  function clamp(v, lo, hi) { return Math.max(lo, Math.min(hi, v)); }

  function clearHideTimer() {
    if (hideTimer) {
      clearTimeout(hideTimer);
      hideTimer = null;
    }
  }

  function scheduleHide() {
    clearHideTimer();
    hideTimer = setTimeout(hideFlyover, 100);
  }

  function showFlyover(trigger, clientX, clientY) {
    const mapSrc = trigger.dataset.mapSrc || "";
    if (!mapSrc) return;

    activeTrigger = trigger;
    anchorX = clientX;
    anchorY = clientY;

    flyImg.src = mapSrc;
    flyImg.alt = trigger.dataset.mapAlt || "Locator map preview";

    const caption = trigger.dataset.caption || "Unknown";
    const filename = trigger.dataset.filename || "Unknown";
    const location = trigger.dataset.location || "Unknown";
    const taken = trigger.dataset.taken || "Unknown";
    const orientation = trigger.dataset.orientation || "unknown";

    flyCap.textContent = caption;
    flySub.textContent = "" + filename + " · " + location + " · " + taken + " · " + orientation;

    flyMeta.textContent = hasModal
      ? "Locator preview · click to magnify"
      : "Locator preview";

    fly.classList.add("visible");
    fly.setAttribute("aria-hidden", "false");

    requestAnimationFrame(() => positionFlyoverFixed(trigger, clientX, clientY));
  }

  function hideFlyover() {
    activeTrigger = null;
    fly.classList.remove("visible");
    fly.setAttribute("aria-hidden", "true");
  }

  function positionFlyoverFixed(trigger, clientX, clientY) {
    if (!fly.classList.contains("visible")) return;

    const vw = window.innerWidth;
    const vh = window.innerHeight;

    const targetW = Math.max(260, Math.min(420, Math.floor(vw * 0.42)));
    fly.style.width = targetW + "px";

    const tr = trigger.getBoundingClientRect();
    const fr = fly.getBoundingClientRect();
    const w = fr.width || targetW;
    const h = fr.height || 220;

    const preferRight = (tr.left + tr.width / 2) < (vw / 2);

    let x = preferRight ? (tr.right - EDGE_OVERLAP) : (tr.left - w + EDGE_OVERLAP);
    let y = clientY - h * 0.35;

    if (clientX < x + POINTER_PAD) x = clientX - POINTER_PAD;
    if (clientX > x + w - POINTER_PAD) x = clientX - w + POINTER_PAD;
    if (clientY < y + POINTER_PAD) y = clientY - POINTER_PAD;
    if (clientY > y + h - POINTER_PAD) y = clientY - h + POINTER_PAD;

    x = clamp(x, MARGIN, vw - w - MARGIN);
    y = clamp(y, MARGIN, vh - h - MARGIN);

    fly.style.left = x + "px";
    fly.style.top = y + "px";
  }

  function openModal(src, alt, trigger) {
    if (!hasModal || !src) return;
    modalImg.src = src;
    modalImg.alt = alt || "Locator map large preview";

    const caption = trigger?.dataset.caption || "Unknown";
    const filename = trigger?.dataset.filename || "Unknown";
    const location = trigger?.dataset.location || "Unknown";
    const taken = trigger?.dataset.taken || "Unknown";
    const orientation = trigger?.dataset.orientation || "unknown";

    modalCap.textContent = caption;
    modalSub.textContent = "" + filename + " · " + location + " · " + taken + " · " + orientation;

    modal.classList.add("open");
    modal.setAttribute("aria-hidden", "false");
    document.body.style.overflow = "hidden";
  }

  function closeModal() {
    if (!hasModal) return;
    modal.classList.remove("open");
    modal.setAttribute("aria-hidden", "true");
    document.body.style.overflow = "";
  }

  triggers.forEach((el) => {
    el.addEventListener("pointerenter", (e) => {
      clearHideTimer();
      showFlyover(el, e.clientX, e.clientY);
    });
    el.addEventListener("pointerleave", scheduleHide);
  });

  fly.addEventListener("pointerenter", clearHideTimer);
  fly.addEventListener("pointerleave", scheduleHide);

  flyImg.addEventListener("click", () => {
    openModal(flyImg.src, flyImg.alt, activeTrigger || flyImg.closest(".photo-trigger"));
  });

  if (hasModal) {
    modalClose.addEventListener("click", closeModal);
    modalBackdrop.addEventListener("click", closeModal);
  }

  window.addEventListener("resize", () => {
    if (activeTrigger && fly.classList.contains("visible")) {
      positionFlyoverFixed(activeTrigger, anchorX, anchorY);
    }
  });

  document.addEventListener("keydown", (e) => {
    if (e.key !== "Escape") return;
    if (hasModal && modal.classList.contains("open")) {
      closeModal();
    } else {
      hideFlyover();
    }
  });
})();
</script>
</body>
</html>`
