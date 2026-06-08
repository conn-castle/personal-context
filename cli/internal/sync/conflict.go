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
	Record    repository.Record
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

	existingByFilename, err := syncengine.FigureMapByFilename(existing)
	if err != nil {
		return FigurePlan{}, fmt.Errorf("existing figures: %w", err)
	}

	desiredByFilename := make(map[string]repository.RecordFigure, len(desired))
	for _, figure := range desired {
		if err := repository.ValidateRecordAssetKey("figures", recordID, figure.Filename, figure.S3Key); err != nil {
			return FigurePlan{}, fmt.Errorf("desired figure asset key: %w", err)
		}
		if _, exists := desiredByFilename[figure.Filename]; exists {
			return FigurePlan{}, fmt.Errorf("duplicate desired figure filename %q", figure.Filename)
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
			RecordID: recordID,
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

	existingByFilename, err := syncengine.DataFileMapByFilename(existing)
	if err != nil {
		return DataFilePlan{}, fmt.Errorf("existing data files: %w", err)
	}

	desiredByFilename := make(map[string]repository.RecordDataFile, len(desired))
	for _, dataFile := range desired {
		if err := repository.ValidateRecordAssetKey("data", recordID, dataFile.Filename, dataFile.S3Key); err != nil {
			return DataFilePlan{}, fmt.Errorf("desired data file asset key: %w", err)
		}
		if strings.TrimSpace(dataFile.Hash) == "" {
			return DataFilePlan{}, fmt.Errorf("desired data file hash is required")
		}
		if _, exists := desiredByFilename[dataFile.Filename]; exists {
			return DataFilePlan{}, fmt.Errorf("duplicate desired data file filename %q", dataFile.Filename)
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
			RecordID:    recordID,
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

func sourceBundleCanRepairTargetChildren(source RecordBundle, target RecordBundle) bool {
	if !recordPayloadsEqual(source.Record, target.Record) {
		return false
	}
	figuresCanRepair := sourceHasExactFigureSuperset(source.Figures, target.Figures)
	dataFilesCanRepair := sourceHasExactDataFileSuperset(source.DataFiles, target.DataFiles)
	if !figuresCanRepair || !dataFilesCanRepair {
		return false
	}
	return len(source.Figures) > len(target.Figures) ||
		len(source.DataFiles) > len(target.DataFiles)
}

func recordPayloadsEqual(left repository.Record, right repository.Record) bool {
	return left.ID == right.ID &&
		left.Date == right.Date &&
		left.DayOrder == right.DayOrder &&
		nullableStringsEqual(left.HTMLContent, right.HTMLContent) &&
		nullableStringsEqual(left.Notes, right.Notes) &&
		left.ProjectID == right.ProjectID &&
		left.SourceDeviceID == right.SourceDeviceID &&
		nullableStringsEqual(left.SourceRef, right.SourceRef) &&
		nullableStringsEqual(left.GitRemoteURL, right.GitRemoteURL) &&
		nullableStringsEqual(left.GitHash, right.GitHash)
}

func nullableStringsEqual(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func sourceHasExactFigureSuperset(source []repository.RecordFigure, target []repository.RecordFigure) bool {
	if len(source) < len(target) {
		return false
	}
	sourceByFilename := make(map[string]repository.RecordFigure, len(source))
	for _, figure := range source {
		sourceByFilename[figure.Filename] = figure
	}
	for _, figure := range target {
		match, ok := sourceByFilename[figure.Filename]
		if !ok {
			return false
		}
		if figure.S3Key != match.S3Key ||
			nullableStringValue(figure.AltText) != nullableStringValue(match.AltText) {
			return false
		}
	}
	return true
}

func sourceHasExactDataFileSuperset(source []repository.RecordDataFile, target []repository.RecordDataFile) bool {
	if len(source) < len(target) {
		return false
	}
	sourceByFilename := make(map[string]repository.RecordDataFile, len(source))
	for _, dataFile := range source {
		sourceByFilename[dataFile.Filename] = dataFile
	}
	for _, dataFile := range target {
		match, ok := sourceByFilename[dataFile.Filename]
		if !ok {
			return false
		}
		if dataFile.S3Key != match.S3Key ||
			dataFile.Size != match.Size ||
			dataFile.Hash != match.Hash ||
			nullableStringValue(dataFile.Description) != nullableStringValue(match.Description) {
			return false
		}
	}
	return true
}
