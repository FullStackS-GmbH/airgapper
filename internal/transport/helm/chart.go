package helm

import (
	"bytes"
	"fmt"
	"os"

	helmloader "helm.sh/helm/v4/pkg/chart/v2/loader"
	chartutil "helm.sh/helm/v4/pkg/chart/v2/util"
)

// prepareChartArchive reads the real chart name from Chart.yaml and repackages
// the archive so its top-level directory and metadata match that name. A
// requested name overrides the name from Chart.yaml.
func prepareChartArchive(data []byte, requestedName string) ([]byte, string, error) {
	chart, err := helmloader.LoadArchive(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("load chart archive: %w", err)
	}
	name := requestedName
	if name == "" {
		name = chart.Metadata.Name
	}
	chart.Metadata.Name = name
	dir, err := os.MkdirTemp("", "airgapper-helm-rename-")
	if err != nil {
		return nil, "", fmt.Errorf("create temporary directory: %w", err)
	}
	defer func() {
		// Temporary cleanup is best-effort after the archive has been read.
		_ = os.RemoveAll(dir)
	}()

	archivePath, err := chartutil.Save(chart, dir)
	if err != nil {
		return nil, "", fmt.Errorf("save prepared chart: %w", err)
	}

	renamed, err := os.ReadFile(archivePath)
	if err != nil {
		return nil, "", fmt.Errorf("read prepared chart: %w", err)
	}
	return renamed, name, nil
}
