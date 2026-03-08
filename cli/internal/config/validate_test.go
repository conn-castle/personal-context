package config

import "testing"

func TestValidateNeonURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"valid postgres URL", "postgres://user:pass@ep-example-123.us-east-1.aws.neon.tech:5432/dbname", false},
		{"valid postgresql scheme", "postgresql://user:pass@host:5432/db", false},
		{"valid with sslmode param", "postgres://user:pass@host:5432/db?sslmode=require", false},
		{"valid neon URL no port", "postgres://user:pass@ep-example.neon.tech/db", false},
		{"empty URL", "", true},
		{"whitespace URL", "   ", true},
		{"not postgres scheme", "mysql://user:pass@host/db", true},
		{"http URL", "http://host:5432/db", true},
		{"no scheme", "user:pass@host:5432/db", true},
		{"missing host", "postgres:///db", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNeonURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateNeonURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestValidateS3Bucket(t *testing.T) {
	tests := []struct {
		name    string
		bucket  string
		wantErr bool
	}{
		{"valid bucket", "my-bucket-123", false},
		{"valid with dots", "my.bucket.name", false},
		{"minimum length", "abc", false},
		{"maximum length 63", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false},
		{"empty", "", true},
		{"whitespace", "  ", true},
		{"too short", "ab", true},
		{"too long 64 chars", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true},
		{"uppercase", "MyBucket", true},
		{"starts with dot", ".bucket", true},
		{"ends with dot", "bucket.", true},
		{"starts with hyphen", "-bucket", true},
		{"ends with hyphen", "bucket-", true},
		{"consecutive dots", "my..bucket", true},
		{"contains slash", "my/bucket", true},
		{"contains underscore", "my_bucket", true},
		{"ip address format", "192.168.1.1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateS3Bucket(tt.bucket)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateS3Bucket(%q) error = %v, wantErr %v", tt.bucket, err, tt.wantErr)
			}
		})
	}
}

func TestValidateS3Region(t *testing.T) {
	tests := []struct {
		name    string
		region  string
		wantErr bool
	}{
		{"us-east-1", "us-east-1", false},
		{"us-gov-west-1", "us-gov-west-1", false},
		{"us-gov-east-1", "us-gov-east-1", false},
		{"us-iso-east-1", "us-iso-east-1", false},
		{"us-west-2", "us-west-2", false},
		{"eu-west-1", "eu-west-1", false},
		{"ap-southeast-1", "ap-southeast-1", false},
		{"sa-east-1", "sa-east-1", false},
		{"ca-central-1", "ca-central-1", false},
		{"me-south-1", "me-south-1", false},
		{"af-south-1", "af-south-1", false},
		{"empty", "", true},
		{"whitespace", "  ", true},
		{"invalid format", "notaregion", true},
		{"uppercase", "US-EAST-1", true},
		{"just letters", "useast", true},
		{"trailing space", "us-east-1 ", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateS3Region(tt.region)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateS3Region(%q) error = %v, wantErr %v", tt.region, err, tt.wantErr)
			}
		})
	}
}

func TestValidateCloudConfig(t *testing.T) {
	t.Run("valid cloud config", func(t *testing.T) {
		err := ValidateCloudConfig(Config{
			NeonURL:    "postgres://user:pass@host:5432/db",
			S3Bucket:   "my-bucket",
			S3Region:   "us-east-1",
			AWSProfile: "personal-context",
		})
		if err != nil {
			t.Fatalf("ValidateCloudConfig() error = %v", err)
		}
	})

	t.Run("local-only config is valid", func(t *testing.T) {
		err := ValidateCloudConfig(Config{})
		if err != nil {
			t.Fatalf("ValidateCloudConfig() error = %v for local-only", err)
		}
	})

	t.Run("partial cloud config fails Mode check", func(t *testing.T) {
		err := ValidateCloudConfig(Config{NeonURL: "postgres://user:pass@host/db"})
		if err == nil {
			t.Fatal("expected error for partial cloud config")
		}
	})

	t.Run("invalid Neon URL fails", func(t *testing.T) {
		err := ValidateCloudConfig(Config{
			NeonURL:    "http://not-postgres",
			S3Bucket:   "my-bucket",
			S3Region:   "us-east-1",
			AWSProfile: "personal-context",
		})
		if err == nil {
			t.Fatal("expected error for invalid Neon URL")
		}
	})

	t.Run("invalid S3 bucket fails", func(t *testing.T) {
		err := ValidateCloudConfig(Config{
			NeonURL:    "postgres://user:pass@host/db",
			S3Bucket:   "INVALID",
			S3Region:   "us-east-1",
			AWSProfile: "personal-context",
		})
		if err == nil {
			t.Fatal("expected error for invalid S3 bucket")
		}
	})

	t.Run("invalid S3 region fails", func(t *testing.T) {
		err := ValidateCloudConfig(Config{
			NeonURL:    "postgres://user:pass@host/db",
			S3Bucket:   "my-bucket",
			S3Region:   "notaregion",
			AWSProfile: "personal-context",
		})
		if err == nil {
			t.Fatal("expected error for invalid S3 region")
		}
	})

	t.Run("empty AWS profile fails", func(t *testing.T) {
		err := ValidateCloudConfig(Config{
			NeonURL:    "postgres://user:pass@host/db",
			S3Bucket:   "my-bucket",
			S3Region:   "us-east-1",
			AWSProfile: "  ",
		})
		if err == nil {
			t.Fatal("expected error for whitespace AWS profile")
		}
	})
}
