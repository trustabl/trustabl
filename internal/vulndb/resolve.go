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

// ResolveConfig drives snapshot resolution.
type ResolveConfig struct {
	// Ecosystems is the set of trustabl ecosystem ids present in the BOM (pypi,
	// npm, golang, nuget, composer, cargo). Only these are fetched, so a
	// Python-only repo never downloads npm's (huge) database.
	Ecosystems []string
	CacheDir   string // empty => os.UserCacheDir()/trustabl/vulndb
	NoUpdate   bool   // offline: use the cache only, never fetch
	HTTPClient *http.Client
}

// Resolved is a loaded, indexed OSV snapshot plus its provenance.
type Resolved struct {
	DB        *DB
	Version   string // content hash of the cached snapshot — folded into ScanID
	FromCache bool   // true if every ecosystem came from cache (no successful fetch)
}

// Resolve loads the OSV snapshot for the BOM's ecosystems: it fetches each
// ecosystem's all.zip (unless NoUpdate), caches it, and falls back to the cache
// when the network is unavailable. The returned Version is a stable hash of the
// exact bytes used, so a scan is honest about which snapshot produced its
// findings.
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

	ecos := uniqueOSVEcosystems(cfg.Ecosystems)
	if len(ecos) == 0 {
		return &Resolved{DB: NewDB(nil), Version: emptyHash()}, nil
	}

	fromCache := true
	var records []Record
	hasher := sha256.New()
	for _, osvEco := range ecos {
		dest := filepath.Join(cacheDir, sanitizeEco(osvEco)+".zip")
		var data []byte
		if !cfg.NoUpdate {
			if fetched, ferr := fetchExport(client, osvEco); ferr == nil {
				if werr := atomicWrite(dest, fetched); werr == nil {
					data, fromCache = fetched, false
				} else {
					data = fetched // use it even if we couldn't cache
					fromCache = false
				}
			}
		}
		if data == nil {
			cached, cerr := os.ReadFile(dest)
			if cerr != nil {
				continue // no fetch and no cache for this ecosystem — skip it
			}
			data = cached
		}
		recs, lerr := loadRecordsFromZip(data)
		if lerr != nil {
			continue
		}
		records = append(records, recs...)
		hasher.Write([]byte(osvEco))
		hasher.Write([]byte{0})
		hasher.Write(data)
	}

	return &Resolved{
		DB:        NewDB(records),
		Version:   hex.EncodeToString(hasher.Sum(nil))[:16],
		FromCache: fromCache,
	}, nil
}

// fetchExport downloads {base}/{osvEcosystem}/all.zip.
func fetchExport(client *http.Client, osvEco string) ([]byte, error) {
	url := fmt.Sprintf("%s/%s/all.zip", osvExportBaseURL, osvEco)
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vulndb: GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
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
