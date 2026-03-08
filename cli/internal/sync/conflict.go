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
)

// SlideBundle is the sync unit for a slide and its child rows.
type SlideBundle struct {
	Slide     repository.Slide
	Figures   []repository.SlideFigure
	DataFiles []repository.SlideDataFile
}

// FigurePlan describes how to reconcile figures using filename matching.
type FigurePlan struct {
	Creates   []repository.CreateSlideFigureInput
	Updates   []repository.UpdateSlideFigureInput
	DeleteIDs []int64
}

// DataFilePlan describes how to reconcile data files using filename matching.
type DataFilePlan struct {
	Creates   []repository.CreateSlideDataFileInput
	Updates   []repository.UpdateSlideDataFileInput
	DeleteIDs []int64
}

// ResolveBundle applies Phase 6 conflict rules and returns the winning bundle.
// Args: local and cloud must describe the same slide ID.
// Returns: the winning bundle, the winning side, or an error.
func ResolveBundle(local SlideBundle, cloud SlideBundle) (SlideBundle, Winner, error) {
	localID := strings.TrimSpace(local.Slide.ID)
	cloudID := strings.TrimSpace(cloud.Slide.ID)
	switch {
	case localID == "":
		return SlideBundle{}, "", fmt.Errorf("local slide id is required")
	case cloudID == "":
		return SlideBundle{}, "", fmt.Errorf("cloud slide id is required")
	case localID != cloudID:
		return SlideBundle{}, "", fmt.Errorf(
			"bundle ids must match: local=%q cloud=%q",
			localID,
			cloudID,
		)
	}

	outcome, err := syncengine.ResolveSlideWinner(&local.Slide, &cloud.Slide)
	if err != nil {
		return SlideBundle{}, "", err
	}

	switch outcome {
	case syncengine.OutcomeLocal:
		return local, WinnerLocal, nil
	case syncengine.OutcomeRemote:
		return cloud, WinnerCloud, nil
	default:
		// Exact same action timestamp and action type: prefer cloud for deterministic convergence.
		return cloud, WinnerCloud, nil
	}
}

// PlanFigureReconciliation matches existing and desired figures by filename.
// Args: slideID is the owning slide; existing are the current target rows; desired are the winning source rows.
// Returns: a create/update/delete plan or an error.
func PlanFigureReconciliation(
	slideID string,
	existing []repository.SlideFigure,
	desired []repository.SlideFigure,
) (FigurePlan, error) {
	if strings.TrimSpace(slideID) == "" {
		return FigurePlan{}, fmt.Errorf("slide id is required")
	}

	plan := FigurePlan{
		Creates:   make([]repository.CreateSlideFigureInput, 0),
		Updates:   make([]repository.UpdateSlideFigureInput, 0),
		DeleteIDs: make([]int64, 0),
	}

	existingByFilename := make(map[string]repository.SlideFigure, len(existing))
	for _, figure := range existing {
		existingByFilename[figure.Filename] = figure
	}

	desiredByFilename := make(map[string]repository.SlideFigure, len(desired))
	for _, figure := range desired {
		if strings.TrimSpace(figure.Filename) == "" || strings.TrimSpace(figure.S3Key) == "" {
			return FigurePlan{}, fmt.Errorf("desired figure filename and s3_key are required")
		}
		desiredByFilename[figure.Filename] = figure
		if existingFigure, ok := existingByFilename[figure.Filename]; ok {
			if existingFigure.S3Key != figure.S3Key ||
				nullableStringValue(existingFigure.AltText) != nullableStringValue(figure.AltText) {
				plan.Updates = append(plan.Updates, repository.UpdateSlideFigureInput{
					ID:       existingFigure.ID,
					Filename: figure.Filename,
					S3Key:    figure.S3Key,
					AltText:  figure.AltText,
				})
			}
			continue
		}
		plan.Creates = append(plan.Creates, repository.CreateSlideFigureInput{
			SlideID:  slideID,
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
// Args: slideID is the owning slide; existing are the current target rows; desired are the winning source rows.
// Returns: a create/update/delete plan or an error.
func PlanDataFileReconciliation(
	slideID string,
	existing []repository.SlideDataFile,
	desired []repository.SlideDataFile,
) (DataFilePlan, error) {
	if strings.TrimSpace(slideID) == "" {
		return DataFilePlan{}, fmt.Errorf("slide id is required")
	}

	plan := DataFilePlan{
		Creates:   make([]repository.CreateSlideDataFileInput, 0),
		Updates:   make([]repository.UpdateSlideDataFileInput, 0),
		DeleteIDs: make([]int64, 0),
	}

	existingByFilename := make(map[string]repository.SlideDataFile, len(existing))
	for _, dataFile := range existing {
		existingByFilename[dataFile.Filename] = dataFile
	}

	desiredByFilename := make(map[string]repository.SlideDataFile, len(desired))
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
				plan.Updates = append(plan.Updates, repository.UpdateSlideDataFileInput{
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
		plan.Creates = append(plan.Creates, repository.CreateSlideDataFileInput{
			SlideID:     slideID,
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
