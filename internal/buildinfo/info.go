package buildinfo

// Info describes the running Gemcp build.
type Info struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
	BuiltAt string `json:"built_at"`
}

func New(version, commit, builtAt string) Info {
	if version == "" {
		version = "dev"
	}
	if commit == "" {
		commit = "unknown"
	}
	if builtAt == "" {
		builtAt = "unknown"
	}
	return Info{
		Name:    "Gemcp",
		Version: version,
		Commit:  commit,
		BuiltAt: builtAt,
	}
}
