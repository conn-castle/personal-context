package repository

import (
	"errors"
	"testing"
)

func validReplaceRecordChildrenInput() ReplaceRecordChildrenInput {
	return ReplaceRecordChildrenInput{
		Record: CreateRecordInput{
			ID:             "20260310-a1b2c3d4",
			Date:           "2026-03-10",
			ProjectID:      "proj-1",
			SourceDeviceID: "device-1",
		},
		Figures: []CreateRecordFigureInput{{
			RecordID: "20260310-a1b2c3d4",
			Filename: "plot.png",
			S3Key:    "figures/20260310-a1b2c3d4/plot.png",
		}},
		DataFiles: []CreateRecordDataFileInput{{
			RecordID: "20260310-a1b2c3d4",
			Filename: "metrics.csv",
			S3Key:    "data/20260310-a1b2c3d4/metrics.csv",
			Size:     12,
			Hash:     "sha256:abc123",
		}},
	}
}

func TestReplaceRecordChildrenInputValidate(t *testing.T) {
	if err := validReplaceRecordChildrenInput().Validate(); err != nil {
		t.Fatalf("Validate(valid) error = %v", err)
	}

	for _, tc := range []struct {
		name    string
		mutate  func(*ReplaceRecordChildrenInput)
		wantErr error
	}{
		{
			name:    "missing record id",
			mutate:  func(input *ReplaceRecordChildrenInput) { input.Record.ID = " " },
			wantErr: ErrInvalidArgument,
		},
		{
			name:    "missing date",
			mutate:  func(input *ReplaceRecordChildrenInput) { input.Record.Date = "" },
			wantErr: ErrInvalidArgument,
		},
		{
			name:    "missing project id",
			mutate:  func(input *ReplaceRecordChildrenInput) { input.Record.ProjectID = "" },
			wantErr: ErrInvalidArgument,
		},
		{
			name:    "missing source device id",
			mutate:  func(input *ReplaceRecordChildrenInput) { input.Record.SourceDeviceID = "" },
			wantErr: ErrInvalidArgument,
		},
		{
			name:    "figure belongs to other record",
			mutate:  func(input *ReplaceRecordChildrenInput) { input.Figures[0].RecordID = "20260310-deadbeef" },
			wantErr: ErrInvalidArgument,
		},
		{
			name: "figure key is not canonical",
			mutate: func(input *ReplaceRecordChildrenInput) {
				input.Figures[0].S3Key = "figures/20260310-a1b2c3d4/wrong.png"
			},
			wantErr: ErrInvalidArgument,
		},
		{
			name:    "data file belongs to other record",
			mutate:  func(input *ReplaceRecordChildrenInput) { input.DataFiles[0].RecordID = "20260310-deadbeef" },
			wantErr: ErrInvalidArgument,
		},
		{
			name:    "data file hash is required",
			mutate:  func(input *ReplaceRecordChildrenInput) { input.DataFiles[0].Hash = " " },
			wantErr: ErrInvalidArgument,
		},
		{
			name:    "data file size cannot be negative",
			mutate:  func(input *ReplaceRecordChildrenInput) { input.DataFiles[0].Size = -1 },
			wantErr: ErrInvalidArgument,
		},
		{
			name: "data file key is not canonical",
			mutate: func(input *ReplaceRecordChildrenInput) {
				input.DataFiles[0].S3Key = "data/20260310-a1b2c3d4/wrong.csv"
			},
			wantErr: ErrInvalidArgument,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := validReplaceRecordChildrenInput()
			tc.mutate(&input)

			err := input.Validate()
			switch {
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("Validate() error = %v, want %v", err, tc.wantErr)
				}
			default:
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
			}
		})
	}
}
