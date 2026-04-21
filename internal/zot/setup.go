package zot

// SetupInput gathers all fields Setup accepts. Structured so optional
// knobs (OpenAlex credentials today, future per-library tuning later)
// can be added without churning every call site.
type SetupInput struct {
	APIKey         string // required
	LibraryID      string // required
	DataDir        string // required
	OpenAlexEmail  string // optional
	OpenAlexAPIKey string // optional
}

// Setup validates inputs, writes config to disk, and returns a SetupResult.
// All interactive prompting happens in the CLI layer (internal/zot/cli) —
// this function is the pure business-logic entry point.
func Setup(in SetupInput) (*SetupResult, error) {
	if err := ValidateAPIKey(in.APIKey); err != nil {
		return nil, err
	}
	if err := ValidateLibraryID(in.LibraryID); err != nil {
		return nil, err
	}
	if err := ValidateDataDir(in.DataDir); err != nil {
		return nil, err
	}

	cfg := &Config{
		APIKey:         in.APIKey,
		LibraryID:      in.LibraryID,
		DataDir:        in.DataDir,
		OpenAlexEmail:  in.OpenAlexEmail,
		OpenAlexAPIKey: in.OpenAlexAPIKey,
	}
	if err := SaveConfig(cfg); err != nil {
		return nil, err
	}

	return &SetupResult{
		OK:        true,
		LibraryID: in.LibraryID,
		DataDir:   in.DataDir,
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
