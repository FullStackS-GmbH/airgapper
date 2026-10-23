package sync

import (
	"github.com/fullstacks-gmbh/airgapper/internal/domain"
	"github.com/fullstacks-gmbh/airgapper/internal/redact"
)

// Report is the machine-readable form of a sync run, printed by
// "sync --output json". It is a separate type from domain.SyncResult so the
// JSON stays stable when internals change, and so the domain package carries no
// presentation concerns.
type Report struct {
	Resources []ResourceReport `json:"resources"`
	Summary   SummaryReport    `json:"summary"`

	// DryRunLog is the path of the dry-run log file, when one was written.
	DryRunLog string `json:"dry_run_log,omitempty"`
}

// ResourceReport is the outcome for one configured resource.
type ResourceReport struct {
	Type        string          `json:"type"`
	Source      string          `json:"source"`
	Destination string          `json:"destination"`
	Versions    []VersionReport `json:"versions"`
}

// VersionReport is the outcome for one tag, chart version or ref.
type VersionReport struct {
	Version string `json:"version"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// SummaryReport holds the totals of a run.
type SummaryReport struct {
	Resources int `json:"resources"`
	Versions  int `json:"versions"`
	Synced    int `json:"synced"`
	Skipped   int `json:"skipped"`
	Failed    int `json:"failed"`
}

// BuildReport converts sync results into a Report. URL credentials in any text
// are masked, as in the text output.
func BuildReport(results []domain.SyncResult) Report {
	summary := Summarize(results)
	report := Report{
		Resources: make([]ResourceReport, 0, len(results)),
		Summary: SummaryReport{
			Resources: summary.TotalResources,
			Versions:  summary.TotalVersions,
			Synced:    summary.Synced,
			Skipped:   summary.Skipped,
			Failed:    summary.Failed,
		},
	}

	for _, r := range results {
		rr := ResourceReport{
			Type:        r.Resource.Type.String(),
			Source:      redact.URLCredentials(r.Resource.Source.String()),
			Destination: redact.URLCredentials(r.Resource.Destination.String()),
			Versions:    make([]VersionReport, 0, r.TotalCount()),
		}
		for _, group := range [][]domain.VersionResult{r.Synced, r.Skipped, r.Failed} {
			for _, vr := range group {
				message := vr.Message
				if message == "" && vr.Error != nil {
					message = vr.Error.Error()
				}
				rr.Versions = append(rr.Versions, VersionReport{
					Version: vr.Version,
					Status:  vr.Status.String(),
					Message: redact.URLCredentials(message),
				})
			}
		}
		report.Resources = append(report.Resources, rr)
	}
	return report
}
