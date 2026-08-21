// Package transport holds helpers shared by every Transporter implementation.
package transport

import (
	"log/slog"

	"github.com/fullstacks-gmbh/airgapper/internal/domain"
)

// SyncVersions runs syncOne for each version and buckets the results by status.
// The caller assigns the Resource field on the returned SyncResult, since some
// transporters resolve the effective destination while syncing.
func SyncVersions(versions []string, syncOne func(version string) (domain.VersionResult, []domain.OperationRecord)) *domain.SyncResult {
	result := &domain.SyncResult{}
	for _, version := range versions {
		vr, ops := syncOne(version)
		result.Operations = append(result.Operations, ops...)
		switch vr.Status {
		case domain.SyncStatusSynced:
			result.Synced = append(result.Synced, vr)
		case domain.SyncStatusSkipped:
			result.Skipped = append(result.Skipped, vr)
		case domain.SyncStatusFailed:
			result.Failed = append(result.Failed, vr)
		}
	}
	return result
}

// DryRunResult reports what a sync would have done for one version without
// mutating anything. Dry-run versions always count as skipped.
func DryRunResult(pushMode domain.PushMode, version string, exists bool, logger *slog.Logger, op func(domain.OperationType, string) domain.OperationRecord) (domain.VersionResult, []domain.OperationRecord) {
	var (
		msg    string
		opType domain.OperationType
	)
	switch {
	case exists && pushMode == domain.PushModeSkip:
		msg, opType = "dry-run: would skip (already exists)", domain.OpSkip
	case exists:
		msg, opType = "dry-run: would overwrite (already exists)", domain.OpOverwrite
	default:
		msg, opType = "dry-run: would sync", domain.OpPush
	}

	logger.Info(msg)
	return domain.VersionResult{Version: version, Status: domain.SyncStatusSkipped, Message: msg},
		[]domain.OperationRecord{op(opType, msg)}
}
