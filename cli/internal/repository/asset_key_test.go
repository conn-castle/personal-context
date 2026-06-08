package repository

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateRecordAssetKey(t *testing.T) {
	validFigure := "figures/20260305-a1b2c3d4/plot.png"
	if err := ValidateRecordAssetKey("figures", "20260305-a1b2c3d4", "plot.png", validFigure); err != nil {
		t.Fatalf("ValidateRecordAssetKey(valid figure) error = %v", err)
	}
	validData := "data/20260305-a1b2c3d4/metrics.csv"
	if err := ValidateRecordAssetKey("data", "20260305-a1b2c3d4", "metrics.csv", validData); err != nil {
		t.Fatalf("ValidateRecordAssetKey(valid data) error = %v", err)
	}

	tests := []struct {
		name     string
		kind     string
		recordID string
		filename string
		s3Key    string
		want     string
	}{
		{
			name:     "unsupported kind",
			kind:     "thumbs",
			recordID: "20260305-a1b2c3d4",
			filename: "plot.png",
			s3Key:    "thumbs/20260305-a1b2c3d4/plot.png",
			want:     "unsupported asset key kind",
		},
		{name: "blank record", kind: "figures", filename: "plot.png", s3Key: validFigure, want: "record id is required"},
		{
			name:     "record path separator",
			kind:     "figures",
			recordID: "nested/record",
			filename: "plot.png",
			s3Key:    "figures/nested/record/plot.png",
			want:     "record id must be one path segment",
		},
		{
			name:     "filename whitespace",
			kind:     "figures",
			recordID: "20260305-a1b2c3d4",
			filename: " plot.png",
			s3Key:    "figures/20260305-a1b2c3d4/ plot.png",
			want:     "filename must not contain surrounding whitespace",
		},
		{
			name:     "blank key",
			kind:     "figures",
			recordID: "20260305-a1b2c3d4",
			filename: "plot.png",
			want:     "s3_key is required",
		},
		{
			name:     "absolute key",
			kind:     "figures",
			recordID: "20260305-a1b2c3d4",
			filename: "plot.png",
			s3Key:    "/figures/20260305-a1b2c3d4/plot.png",
			want:     "forward-slash relative path",
		},
		{
			name:     "wrong shape",
			kind:     "figures",
			recordID: "20260305-a1b2c3d4",
			filename: "plot.png",
			s3Key:    "figures/plot.png",
			want:     "must be figures/{record_id}/{filename}",
		},
		{
			name:     "wrong owner",
			kind:     "data",
			recordID: "20260305-a1b2c3d4",
			filename: "metrics.csv",
			s3Key:    "data/20260305-deadbeef/metrics.csv",
			want:     "must equal",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRecordAssetKey(tc.kind, tc.recordID, tc.filename, tc.s3Key)
			if !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("ValidateRecordAssetKey() error = %v, want ErrInvalidArgument", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateRecordAssetKey() error = %v, want %q", err, tc.want)
			}
		})
	}
}
