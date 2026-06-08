package vulndb

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// osvExportBaseURL is the OSV.dev bulk export bucket. Each supported ecosystem
// publishes a single all.zip of every record. We pull a PINNED snapshot of these
// (cached locally, content-hashed) and match against it — the scan never queries
// the OSV API, so determinism and the no-network contract hold.
const osvExportBaseURL = "https://osv-vulnerabilities.storage.googleapis.com"

// DefaultMaxAge is how long a cached OSV export is reused before --vuln-scan
// re-fetches it. A day keeps repeated scans fast (a Python repo's PyPI export is
// ~23 MB; npm's is far larger) while staying reasonably fresh. `vulndb pull`
// (ForceRefresh) refreshes on demand; --no-rules-update (NoUpdate) pins to the
// cache at any age.
const DefaultMaxAge = 24 * time.Hour

// ResolveConfig drives snapshot resolution.
type ResolveConfig struct {
	// Ecosystems is the set of trustabl ecosystem ids present in the BOM (pypi,
	// npm, golang, nuget, composer, cargo). Only these are fetched, so a
	// Python-only repo never downloads npm's (huge) database.
	Ecosystems []string
	CacheDir   string // empty => os.UserCacheDir()/trustabl/vulndb
	NoUpdate   bool   // offline: use the cache only, never fetch (at any age)
	// ForceRefresh fetches every ecosystem even if a fresh cache exists — used by
	// `vulndb pull` to refresh the pinned snapshot on demand.
	ForceRefresh bool
	// MaxAge reuses a cached ecosystem export younger than this without
	// re-fetching; an older or missing one is fetched (unless NoUpdate). Zero
	// selects DefaultMaxAge. This is what makes repeated --vuln-scan runs reuse
	// the cached download instead of pulling tens of MB every time.
	MaxAge     time.Duration
	BaseURL    string // OSV export base; empty => osvExportBaseURL (overridable for a mirror or tests)
	HTTPClient *http.Client
	// OnProgress, if set, receives per-ecosystem resolution events for a UI
	// (progress bar / status line). It is advisory only — it never affects the
	// resolved snapshot or its Version, so determinism is preserved.
	OnProgress func(ResolveProgress)
}

// ResolveProgress reports per-ecosystem resolution progress to the optional
// ResolveConfig.OnProgress hook. It fires once when an ecosystem starts, again
// for roughly every 1 MiB downloaded, and once when the ecosystem finishes
// (loaded from the network or the cache, or skipped when neither is available).
type ResolveProgress struct {
	OSVEcosystem string // OSV export name being resolved, e.g. "PyPI", "npm"
	Index        int    // 1-based position in the resolve order
	Total        int    // total ecosystems being resolved
	BytesRead    int64  // cumulative bytes downloaded so far (0 if cached / just started)
	Finished     bool   // the ecosystem is done (data loaded or skipped)
	FromCache    bool   // Finished: loaded from cache, no successful fetch
	Records      int    // Finished: number of advisory records loaded
}

// Resolved is a loaded, indexed OSV snapshot plus its provenance.
type Resolved struct {
	DB        *DB
	Version   string // content hash of the cached snapshot — folded into ScanID
	FromCache bool   // true if every ecosystem came from cache (no successful fetch)
}

// Resolve loads the OSV snapshot for the BOM's ecosystems. It is CACHE-FIRST:
// for each ecosystem it reuses the cached all.zip when present and younger than
// MaxAge, fetching only a missing or stale one (so repeated --vuln-scan runs
// don't re-download tens of MB each time). NoUpdate pins to the cache at any age;
// ForceRefresh always fetches. A fetch failure falls back to whatever is cached.
// The returned Version is a stable hash of the exact bytes used, so a scan is
// honest about which snapshot produced its findings.
func Resolve(cfg ResolveConfig) (*Resolved, error) {
	cacheDir := cfg.CacheDir
	if cacheDir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return nil, fmt.Errorf("vulndb: locate cache dir: %w", err)
		}
		cacheDir = filepath.Join(base, "trustabl", "vulndb")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 180 * time.Second}
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = osvExportBaseURL
	}
	maxAge := cfg.MaxAge
	if maxAge <= 0 {
		maxAge = DefaultMaxAge
	}

	ecos := uniqueOSVEcosystems(cfg.Ecosystems)
	if len(ecos) == 0 {
		return &Resolved{DB: NewDB(nil), Version: emptyHash()}, nil
	}

	fromCache := true
	var records []Record
	hasher := sha256.New()
	for i, osvEco := range ecos {
		report := func(p ResolveProgress) {
			if cfg.OnProgress != nil {
				p.OSVEcosystem, p.Index, p.Total = osvEco, i+1, len(ecos)
				cfg.OnProgress(p)
			}
		}
		report(ResolveProgress{}) // ecosystem started

		dest := filepath.Join(cacheDir, sanitizeEco(osvEco)+".zip")
		var data []byte
		thisFromCache := true
		// Cache-first: fetch only when online AND (forced, or the cache is missing
		// or stale). A fresh cache is reused without touching the network.
		if !cfg.NoUpdate && (cfg.ForceRefresh || cacheStale(dest, maxAge)) {
			onBytes := func(n int64) { report(ResolveProgress{BytesRead: n}) }
			if fetched, ferr := fetchExport(client, baseURL, osvEco, onBytes); ferr == nil {
				thisFromCache = false
				_ = atomicWrite(dest, fetched) // cache best-effort; use the bytes regardless
				data = fetched
			}
		}
		if data == nil {
			cached, cerr := os.ReadFile(dest)
			if cerr != nil {
				report(ResolveProgress{Finished: true, FromCache: true}) // no fetch, no cache — skipped
				continue
			}
			data = cached
		}
		recs, lerr := loadRecordsFromZip(data)
		if lerr != nil {
			report(ResolveProgress{Finished: true, FromCache: thisFromCache})
			continue
		}
		if !thisFromCache {
			fromCache = false
		}
		records = append(records, recs...)
		hasher.Write([]byte(osvEco))
		hasher.Write([]byte{0})
		hasher.Write(data)
		report(ResolveProgress{Finished: true, FromCache: thisFromCache, Records: len(recs)})
	}

	return &Resolved{
		DB:        NewDB(records),
		Version:   hex.EncodeToString(hasher.Sum(nil))[:16],
		FromCache: fromCache,
	}, nil
}

// cacheStale reports whether the cached export at path is missing or older than
// maxAge and so should be re-fetched. This is a cache-freshness decision based on
// file mtime only — it never affects the resolved snapshot's Version (the content
// hash of the bytes used) or the report, so determinism holds.
func cacheStale(path string, maxAge time.Duration) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return true // missing or unreadable → fetch
	}
	return time.Since(fi.ModTime()) > maxAge
}

// fetchExport downloads {base}/{osvEcosystem}/all.zip, streaming byte progress
// to onBytes (which may be nil) so a UI can show download status.
func fetchExport(client *http.Client, base, osvEco string, onBytes func(int64)) ([]byte, error) {
	url := fmt.Sprintf("%s/%s/all.zip", base, osvEco)
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vulndb: GET %s: %s", url, resp.Status)
	}
	return readAllProgress(resp.Body, onBytes)
}

// readAllProgress reads r to completion, invoking onBytes with the cumulative
// byte count at ~1 MiB granularity (and once more with the exact final total) so
// a UI can show download progress without being flooded. onBytes may be nil. The
// returned bytes are identical to io.ReadAll(r) — progress never alters the data,
// so the snapshot hash (and thus ScanID) is unaffected.
func readAllProgress(r io.Reader, onBytes func(int64)) ([]byte, error) {
	var buf bytes.Buffer
	chunk := make([]byte, 64*1024)
	var total, reported int64
	for {
		n, rerr := r.Read(chunk)
		if n > 0 {
			buf.Write(chunk[:n])
			total += int64(n)
			if onBytes != nil && total-reported >= 1<<20 {
				onBytes(total)
				reported = total
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return nil, rerr
		}
	}
	if onBytes != nil && total != reported {
		onBytes(total)
	}
	return buf.Bytes(), nil
}

// loadRecordsFromZip parses every *.json entry of an OSV all.zip into a Record.
func loadRecordsFromZip(data []byte) ([]Record, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !strings.HasSuffix(f.Name, ".json") {
			continue
		}
		rc, oerr := f.Open()
		if oerr != nil {
			continue
		}
		raw, rerr := io.ReadAll(rc)
		rc.Close()
		if rerr != nil {
			continue
		}
		var rec Record
		if json.Unmarshal(raw, &rec) == nil && rec.ID != "" {
			out = append(out, rec)
		}
	}
	return out, nil
}

// uniqueOSVEcosystems maps the BOM's trustabl ecosystems to OSV export names,
// de-duplicated and sorted for a stable fetch + hash order.
func uniqueOSVEcosystems(ecosystems []string) []string {
	set := map[string]struct{}{}
	for _, e := range ecosystems {
		if osv, ok := osvEcosystem[e]; ok {
			set[osv] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for e := range set {
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}

func sanitizeEco(osvEco string) string {
	return strings.NewReplacer("/", "_", ".", "_", " ", "_").Replace(osvEco)
}

func atomicWrite(dest string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".vulndb-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, werr := tmp.Write(data); werr != nil {
		tmp.Close()
		os.Remove(tmpName)
		return werr
	}
	if cerr := tmp.Close(); cerr != nil {
		os.Remove(tmpName)
		return cerr
	}
	return os.Rename(tmpName, dest)
}

func emptyHash() string {
	h := sha256.Sum256(nil)
	return hex.EncodeToString(h[:])[:16]
}
