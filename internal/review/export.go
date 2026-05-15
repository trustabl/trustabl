package review

import (
	"archive/zip"
	"fmt"
	"os"

	"github.com/trustabl/karenctl/internal/models"
)

// ExportZIP packages the generated artifacts into a single ZIP at outPath.
//
// Layout mirrors what `--apply` would write into the repo, so the ZIP can be
// extracted on top of a repo as a manual alternative to running --apply.
func ExportZIP(outPath string, artifacts []models.GeneratedArtifact) error {
	if len(artifacts) == 0 {
		return fmt.Errorf("no artifacts to export")
	}
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", outPath, err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	for _, a := range artifacts {
		w, err := zw.Create(a.RelativePath)
		if err != nil {
			return fmt.Errorf("zip entry %s: %w", a.RelativePath, err)
		}
		if _, err := w.Write([]byte(a.Contents)); err != nil {
			return fmt.Errorf("write entry %s: %w", a.RelativePath, err)
		}
	}
	return nil
}
