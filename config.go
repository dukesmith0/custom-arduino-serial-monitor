package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const configDirName = "custom-arduino-serial-monitor"
const templatesFileName = "templates.json"

// configDir returns the path to the app's config directory in %APPDATA%.
func configDir() (string, error) {
	appData, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get config dir: %w", err)
	}
	dir := filepath.Join(appData, configDirName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create config dir: %w", err)
	}
	return dir, nil
}

// LoadTemplates reads saved templates from disk. Returns empty slice if file doesn't exist.
func LoadTemplates() ([]string, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}

	path := filepath.Join(dir, templatesFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to read templates: %w", err)
	}

	var templates []string
	if err := json.Unmarshal(data, &templates); err != nil {
		return nil, fmt.Errorf("failed to parse templates: %w", err)
	}
	return templates, nil
}

// SaveTemplates writes templates to disk.
func SaveTemplates(templates []string) error {
	dir, err := configDir()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(templates, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal templates: %w", err)
	}

	path := filepath.Join(dir, templatesFileName)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write templates: %w", err)
	}
	return nil
}
