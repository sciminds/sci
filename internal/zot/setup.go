package zot

// Setup validates inputs, writes config to disk, and returns a SetupResult.
// All interactive prompting happens in the CLI layer (internal/zot/cli) —
// this function is the pure business-logic entry point.
func Setup(apiKey, libraryID, dataDir string) (*SetupResult, error) {
	if err := ValidateAPIKey(apiKey); err != nil {
		return nil, err
	}
	if err := ValidateLibraryID(libraryID); err != nil {
		return nil, err
	}
	if err := ValidateDataDir(dataDir); err != nil {
		return nil, err
	}

	cfg := &Config{APIKey: apiKey, LibraryID: libraryID, DataDir: dataDir}
	if err := SaveConfig(cfg); err != nil {
		return nil, err
	}

	return &SetupResult{
		OK:        true,
		LibraryID: libraryID,
		DataDir:   dataDir,
		Message:   "zot configured",
	}, nil
}

// Logout clears saved credentials.
func Logout() (*SetupResult, error) {
	if err := ClearConfig(); err != nil {
		return nil, err
	}
	return &SetupResult{OK: true, Message: "zot credentials cleared"}, nil
}
