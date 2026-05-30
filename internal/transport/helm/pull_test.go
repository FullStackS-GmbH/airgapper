package helm_test

import (
	"testing"

	"github.com/fullstacks-gmbh/airgapper/internal/transport/helm"
)

func TestPullChartBytes_Exists(t *testing.T) {
	// Verifies PullChartBytes is exported and has the right signature.
	var h *helm.Transporter
	_ = h.PullChartBytes // must compile
}
