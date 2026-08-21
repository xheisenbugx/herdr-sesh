package main

import "fmt"

type sourceGlyph struct {
	icon  string
	color int
}

var sourceGlyphs = map[string]sourceGlyph{
	"herdr":  {icon: "●", color: 34},
	"config": {icon: "⚙", color: 90},
	"zoxide": {icon: "", color: 36},
	"find":   {icon: "⌕", color: 32},
}

type Candidate struct {
	Kind           string   `json:"kind"`
	Name           string   `json:"name"`
	Path           string   `json:"path"`
	WorkspaceID    string   `json:"workspace_id,omitempty"`
	Alias          string   `json:"alias,omitempty"`
	Icon           string   `json:"icon,omitempty"`
	Score          float64  `json:"score,omitempty"`
	Tabs           []string `json:"-"`
	StartupCommand string   `json:"-"`
	PreviewCommand string   `json:"-"`
	DisableStartup bool     `json:"-"`
}

func (c Candidate) displayName(showIcons bool) string {
	prefix := ""
	if showIcons {
		glyph, hasGlyph := sourceGlyphs[c.Kind]
		icon := c.Icon
		if icon == "" {
			icon = glyph.icon
		}
		if icon != "" {
			if hasGlyph {
				icon = fmt.Sprintf("\x1b[%dm%s\x1b[39m", glyph.color, icon)
			}
			prefix = icon + " "
		}
	}
	alias := ""
	if c.Alias != "" {
		alias = c.Alias + "  "
	}
	return prefix + alias + c.Name
}
