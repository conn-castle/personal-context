package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/conn-castle/personal-context/cli/internal/repository"
)

func resolveRecordProvenance(
	metadataProjectID *string,
	metadataDeviceID *string,
	metadataSourceRef *string,
	projectFlag string,
	deviceFlag string,
	sourceRefFlag string,
) (string, string, *string, error) {
	projectID, err := resolveRequiredProvenanceValue("project_id", metadataProjectID, projectFlag)
	if err != nil {
		return "", "", nil, err
	}
	deviceID, err := resolveRequiredProvenanceValue("source_device_id", metadataDeviceID, deviceFlag)
	if err != nil {
		return "", "", nil, err
	}
	sourceRef, err := resolveOptionalProvenanceValue("source_ref", metadataSourceRef, sourceRefFlag)
	if err != nil {
		return "", "", nil, err
	}
	return projectID, deviceID, sourceRef, nil
}

func resolveRequiredProvenanceValue(field string, metadataValue *string, flagValue string) (string, error) {
	flagValue = strings.TrimSpace(flagValue)
	if metadataValue != nil {
		metadataTrimmed := strings.TrimSpace(*metadataValue)
		if metadataTrimmed == "" {
			return "", fmt.Errorf("%s in metadata.json must not be empty", field)
		}
		if flagValue != "" && flagValue != metadataTrimmed {
			return "", fmt.Errorf("%s conflict: CLI flag %q does not match metadata.json %q", field, flagValue, metadataTrimmed)
		}
		return metadataTrimmed, nil
	}
	if flagValue == "" {
		return "", fmt.Errorf("%s is required; provide --%s or metadata.json", field, provenanceFlagName(field))
	}
	return flagValue, nil
}

func resolveOptionalProvenanceValue(field string, metadataValue *string, flagValue string) (*string, error) {
	flagValue = strings.TrimSpace(flagValue)
	if metadataValue != nil {
		metadataTrimmed := strings.TrimSpace(*metadataValue)
		if flagValue != "" && flagValue != metadataTrimmed {
			return nil, fmt.Errorf("%s conflict: CLI flag %q does not match metadata.json %q", field, flagValue, metadataTrimmed)
		}
		if metadataTrimmed == "" {
			return nil, nil
		}
		return &metadataTrimmed, nil
	}
	if flagValue == "" {
		return nil, nil
	}
	return &flagValue, nil
}

func provenanceFlagName(field string) string {
	if field == "project_id" {
		return "project"
	}
	if field == "source_device_id" {
		return "device"
	}
	return strings.ReplaceAll(field, "_", "-")
}

func validateActiveProjectAndDevice(ctx context.Context, repo repository.Repository, projectID string, deviceID string) error {
	project, err := repo.GetProjectByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("project %q is not registered; run `pc project list` or `pc project add %s`", projectID, projectID)
		}
		return fmt.Errorf("get project %q: %w", projectID, err)
	}
	if project.ArchivedAt != nil {
		return fmt.Errorf("project %q is archived; run `pc project restore %s` before using it", projectID, projectID)
	}
	device, err := repo.GetDeviceByID(ctx, deviceID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("device %q is not registered; run `pc device list` or `pc device register %s`", deviceID, deviceID)
		}
		return fmt.Errorf("get device %q: %w", deviceID, err)
	}
	if device.ArchivedAt != nil {
		return fmt.Errorf("device %q is archived; run `pc device restore %s` before using it", deviceID, deviceID)
	}
	return nil
}
