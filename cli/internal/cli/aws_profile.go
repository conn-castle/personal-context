package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	awsProfileDirPermission  = 0o700
	awsProfileFilePermission = 0o600
)

// awsCredentialsPath returns the shared credentials file path rooted at homeDir.
func awsCredentialsPath(homeDir string) string {
	return filepath.Join(homeDir, ".aws", "credentials")
}

// currentUserAWSCredentialsPath resolves the real shared-credentials path for the OS user.
func currentUserAWSCredentialsPath() (string, error) {
	homeDir, err := resolveUserHomeDir()
	if err != nil {
		return "", err
	}
	return awsCredentialsPath(homeDir), nil
}

// writeAWSSharedCredentialsProfile upserts a named profile in ~/.aws/credentials.
func writeAWSSharedCredentialsProfile(homeDir string, profileName string, accessKeyID string, secretAccessKey string) error {
	if strings.TrimSpace(homeDir) == "" {
		return fmt.Errorf("home directory is required")
	}
	if strings.TrimSpace(profileName) == "" {
		return fmt.Errorf("profile name is required")
	}
	if strings.TrimSpace(accessKeyID) == "" {
		return fmt.Errorf("aws access key id is required")
	}
	if strings.TrimSpace(secretAccessKey) == "" {
		return fmt.Errorf("aws secret access key is required")
	}

	path := awsCredentialsPath(homeDir)
	lines, err := readINIFileLines(path)
	if err != nil {
		if os.IsNotExist(err) {
			lines = []string{}
		} else {
			return err
		}
	}

	profileLines := []string{
		"[" + profileName + "]",
		"aws_access_key_id = " + accessKeyID,
		"aws_secret_access_key = " + secretAccessKey,
	}

	updated := upsertINISection(lines, profileName, profileLines)
	content := strings.Join(updated, "\n")
	if content != "" {
		content += "\n"
	}
	return writeTextFileAtomically(path, []byte(content), awsProfileDirPermission, awsProfileFilePermission)
}

// removeAWSSharedCredentialsProfile removes a named profile from ~/.aws/credentials.
func removeAWSSharedCredentialsProfile(homeDir string, profileName string) error {
	if strings.TrimSpace(homeDir) == "" {
		return fmt.Errorf("home directory is required")
	}
	if strings.TrimSpace(profileName) == "" {
		return fmt.Errorf("profile name is required")
	}

	path := awsCredentialsPath(homeDir)
	lines, err := readINIFileLines(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	updated, removed := removeINISection(lines, profileName)
	if !removed {
		return nil
	}

	content := strings.Join(updated, "\n")
	if content != "" {
		content += "\n"
	}
	return writeTextFileAtomically(path, []byte(content), awsProfileDirPermission, awsProfileFilePermission)
}

func readINIFileLines(path string) ([]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, err
		}
		return nil, fmt.Errorf("read ini file %s: %w", path, err)
	}
	text := strings.TrimRight(string(content), "\n")
	if text == "" {
		return []string{}, nil
	}
	return strings.Split(text, "\n"), nil
}

func upsertINISection(lines []string, sectionName string, newSection []string) []string {
	updated, removed := removeINISection(lines, sectionName)
	if removed && len(updated) > 0 && updated[len(updated)-1] == "" {
		updated = trimTrailingBlankLines(updated)
	}
	if len(updated) > 0 {
		updated = append(updated, "")
	}
	updated = append(updated, newSection...)
	return updated
}

func removeINISection(lines []string, sectionName string) ([]string, bool) {
	if len(lines) == 0 {
		return []string{}, false
	}

	header := "[" + sectionName + "]"
	start := -1
	end := len(lines)
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if start == -1 {
			if trimmed == header {
				start = index
			}
			continue
		}
		if isINISectionHeader(trimmed) {
			end = index
			break
		}
	}
	if start == -1 {
		return append([]string{}, lines...), false
	}

	updated := append([]string{}, lines[:start]...)
	if end < len(lines) {
		updated = append(updated, lines[end:]...)
	}
	return trimLeadingAndTrailingBlankLines(updated), true
}

func isINISectionHeader(line string) bool {
	return strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]")
}

func trimLeadingAndTrailingBlankLines(lines []string) []string {
	lines = trimLeadingBlankLines(lines)
	return trimTrailingBlankLines(lines)
}

func trimLeadingBlankLines(lines []string) []string {
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	return append([]string{}, lines[start:]...)
}

func trimTrailingBlankLines(lines []string) []string {
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return append([]string{}, lines[:end]...)
}
