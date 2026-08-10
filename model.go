package main

type Candidate struct {
	Kind           string   `json:"kind"`
	Name           string   `json:"name"`
	Path           string   `json:"path"`
	WorkspaceID    string   `json:"workspace_id,omitempty"`
	Alias          string   `json:"alias,omitempty"`
	Icon           string   `json:"icon,omitempty"`
	Score          float64  `json:"score,omitempty"`
	Windows        []string `json:"-"`
	StartupCommand string   `json:"-"`
	PreviewCommand string   `json:"-"`
	DisableStartup bool     `json:"-"`
}

func (c Candidate) displayName(showIcons bool) string {
	prefix := ""
	if showIcons {
		icon := c.Icon
		if icon == "" {
			icon = map[string]string{"herdr": "●", "config": "⚙", "zoxide": "", "find": "⌕"}[c.Kind]
		}
		if icon != "" {
			prefix = icon + " "
		}
	}
	alias := ""
	if c.Alias != "" {
		alias = c.Alias + "  "
	}
	return prefix + alias + c.Name
}
