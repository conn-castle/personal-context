package sync

import (
	"fmt"
	"strings"

	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/conn-castle/personal-context/cli/internal/syncengine"
)

// Winner identifies which side won conflict resolution.
type Winner string

const (
	// WinnerLocal means the local bundle won.
	WinnerLocal Winner = "local"
	// WinnerCloud means the cloud bundle won.
	WinnerCloud Winner = "cloud"
	// WinnerNone means both sides are equal; neither should overwrite the other.
	WinnerNone Winner = "none"
)

// RecordBundle is the sync unit for a record and its child rows.
type RecordBundle struct {
	Record     repository.Record
	Figures   []repository.RecordFigure
	DataFiles []repository.RecordDataFile
}

// FigurePlan describes how to reconcile figures using filename matching.
type FigurePlan struct {
	Creates   []repository.CreateRecordFigureInput
	Updates   []repository.UpdateRecordFigureInput
	DeleteIDs []int64
}

// DataFilePlan describes how to reconcile data files using filename matching.
type DataFilePlan struct {
	Creates   []repository.CreateRecordDataFileInput
	Updates   []repository.UpdateRecordDataFileInput
	DeleteIDs []int64
}

// ResolveBundle applies Phase 6 conflict rules and returns the winning bundle.
// Args: local and cloud must describe the same record ID.
// Returns: the winning bundle, the winning side, or an error.
func ResolveBundle(local RecordBundle, cloud RecordBundle) (RecordBundle, Winner, error) {
	localID := strings.TrimSpace(local.Record.ID)
	cloudID := strings.TrimSpace(cloud.Record.ID)
	switch {
	case localID == "":
		return RecordBundle{}, "", fmt.Errorf("local record id is required")
	case cloudID == "":
		return RecordBundle{}, "", fmt.Errorf("cloud record id is required")
	case localID != cloudID:
		return RecordBundle{}, "", fmt.Errorf(
			"bundle ids must match: local=%q cloud=%q",
			localID,
			cloudID,
		)
	}

	outcome, err := syncengine.ResolveRecordWinner(&local.Record, &cloud.Record)
	if err != nil {
		return RecordBundle{}, "", err
	}

	switch outcome {
	case syncengine.OutcomeLocal:
		return local, WinnerLocal, nil
	case syncengine.OutcomeRemote:
		return cloud, WinnerCloud, nil
	default:
		// Equal state: skip both push and pull. This prevents a partial push/pull failure
		// from overwriting the complete side with the incomplete side on the next sync.
		return RecordBundle{}, WinnerNone, nil
	}
}

// PlanFigureReconciliation matches existing and desired figures by filename.
// Args: recordID is the owning record; existing are the current target rows; desired are the winning source rows.
// Returns: a create/update/delete plan or an error.
func PlanFigureReconciliation(
	recordID string,
	existing []repository.RecordFigure,
	desired []repository.RecordFigure,
) (FigurePlan, error) {
	if strings.TrimSpace(recordID) == "" {
		return FigurePlan{}, fmt.Errorf("record id is required")
	}

	plan := FigurePlan{
		Creates:   make([]repository.CreateRecordFigureInput, 0),
		Updates:   make([]repository.UpdateRecordFigureInput, 0),
		DeleteIDs: make([]int64, 0),
	}

	existingByFilename := make(map[string]repository.RecordFigure, len(existing))
	for _, figure := range existing {
		existingByFilename[figure.Filename] = figure
	}

	desiredByFilename := make(map[string]repository.RecordFigure, len(desired))
	for _, figure := range desired {
		if strings.TrimSpace(figure.Filename) == "" || strings.TrimSpace(figure.S3Key) == "" {
			return FigurePlan{}, fmt.Errorf("desired figure filename and s3_key are required")
		}
		desiredByFilename[figure.Filename] = figure
		if existingFigure, ok := existingByFilename[figure.Filename]; ok {
			if existingFigure.S3Key != figure.S3Key ||
				nullableStringValue(existingFigure.AltText) != nullableStringValue(figure.AltText) {
				plan.Updates = append(plan.Updates, repository.UpdateRecordFigureInput{
					ID:       existingFigure.ID,
					Filename: figure.Filename,
					S3Key:    figure.S3Key,
					AltText:  figure.AltText,
				})
			}
			continue
		}
		plan.Creates = append(plan.Creates, repository.CreateRecordFigureInput{
			RecordID:  recordID,
			Filename: figure.Filename,
			S3Key:    figure.S3Key,
			AltText:  figure.AltText,
		})
	}

	for _, figure := range existing {
		if _, ok := desiredByFilename[figure.Filename]; !ok {
			plan.DeleteIDs = append(plan.DeleteIDs, figure.ID)
		}
	}

	return plan, nil
}

// PlanDataFileReconciliation matches existing and desired data files by filename.
// Args: recordID is the owning record; existing are the current target rows; desired are the winning source rows.
// Returns: a create/update/delete plan or an error.
func PlanDataFileReconciliation(
	recordID string,
	existing []repository.RecordDataFile,
	desired []repository.RecordDataFile,
) (DataFilePlan, error) {
	if strings.TrimSpace(recordID) == "" {
		return DataFilePlan{}, fmt.Errorf("record id is required")
	}

	plan := DataFilePlan{
		Creates:   make([]repository.CreateRecordDataFileInput, 0),
		Updates:   make([]repository.UpdateRecordDataFileInput, 0),
		DeleteIDs: make([]int64, 0),
	}

	existingByFilename := make(map[string]repository.RecordDataFile, len(existing))
	for _, dataFile := range existing {
		existingByFilename[dataFile.Filename] = dataFile
	}

	desiredByFilename := make(map[string]repository.RecordDataFile, len(desired))
	for _, dataFile := range desired {
		if strings.TrimSpace(dataFile.Filename) == "" ||
			strings.TrimSpace(dataFile.S3Key) == "" ||
			strings.TrimSpace(dataFile.Hash) == "" {
			return DataFilePlan{}, fmt.Errorf(
				"desired data file filename, s3_key, and hash are required",
			)
		}
		desiredByFilename[dataFile.Filename] = dataFile
		if existingDataFile, ok := existingByFilename[dataFile.Filename]; ok {
			if existingDataFile.S3Key != dataFile.S3Key ||
				existingDataFile.Size != dataFile.Size ||
				existingDataFile.Hash != dataFile.Hash ||
				nullableStringValue(existingDataFile.Description) !=
					nullableStringValue(dataFile.Description) {
				size := dataFile.Size
				hash := dataFile.Hash
				plan.Updates = append(plan.Updates, repository.UpdateRecordDataFileInput{
					ID:          existingDataFile.ID,
					Filename:    dataFile.Filename,
					S3Key:       dataFile.S3Key,
					Size:        &size,
					Hash:        &hash,
					Description: dataFile.Description,
				})
			}
			continue
		}
		plan.Creates = append(plan.Creates, repository.CreateRecordDataFileInput{
			RecordID:     recordID,
			Filename:    dataFile.Filename,
			S3Key:       dataFile.S3Key,
			Size:        dataFile.Size,
			Hash:        dataFile.Hash,
			Description: dataFile.Description,
		})
	}

	for _, dataFile := range existing {
		if _, ok := desiredByFilename[dataFile.Filename]; !ok {
			plan.DeleteIDs = append(plan.DeleteIDs, dataFile.ID)
		}
	}

	return plan, nil
}

func nullableStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
