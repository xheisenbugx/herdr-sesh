package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Service struct {
	cfg          Config
	client       *HerdrClient
	configFile   string
	projectCache map[string]projectCacheEntry
}

type projectCacheEntry struct {
	project gitProject
	found   bool
}

func newService(cfg Config) (*Service, error) {
	// Directory listing and previews do not require a running Herdr server.
	c := &HerdrClient{socketPath: os.Getenv("HERDR_SOCKET_PATH")}
	return &Service{cfg: cfg, client: c, projectCache: map[string]projectCacheEntry{}}, nil
}

func (s *Service) List(kinds map[string]bool, includeBlacklisted bool) ([]Candidate, error) {
	var all []Candidate
	type frecencyResult struct {
		candidates []Candidate
		err        error
	}
	var recent chan frecencyResult
	if len(kinds) == 0 || kinds["zoxide"] {
		recent = make(chan frecencyResult, 1)
		go func() {
			cs, err := listFrecency(s.cfg.Frecency)
			recent <- frecencyResult{cs, err}
		}()
	}
	if len(kinds) == 0 || kinds["herdr"] {
		snap, err := s.client.Snapshot()
		if err != nil {
			return nil, err
		}
		all = append(all, s.activeCandidates(snap)...)
	}
	if len(kinds) == 0 || kinds["config"] {
		cs, err := s.configCandidates()
		if err != nil {
			return nil, err
		}
		all = append(all, cs...)
	}
	if recent != nil {
		result := <-recent
		if result.err != nil {
			return nil, result.err
		}
		zs := result.candidates
		for i := range zs {
			// Like sesh, display the actual directory, at any depth. Git naming
			// belongs to workspace creation, not this frequently executed path.
			zs[i].Name = shortPath(zs[i].Path)
			s.enrich(&zs[i])
			all = append(all, zs[i])
		}
	}
	filtered := all[:0]
	for _, c := range all {
		if isBlacklisted(c, s.cfg.Blacklist) == includeBlacklisted {
			filtered = append(filtered, c)
		}
	}
	filtered = s.dedupeCandidates(filtered)
	order := map[string]int{}
	for i, k := range s.cfg.SortOrder {
		order[k] = i
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		oi, ok := order[filtered[i].Kind]
		if !ok {
			oi = 99
		}
		oj, ok := order[filtered[j].Kind]
		if !ok {
			oj = 99
		}
		if oi != oj {
			return oi < oj
		}
		if filtered[i].Kind != filtered[j].Kind {
			return filtered[i].Kind < filtered[j].Kind
		}
		if filtered[i].Kind == "zoxide" {
			// Preserve the backend's ranking, including equal-score ordering.
			return false
		}
		return strings.ToLower(filtered[i].Name) < strings.ToLower(filtered[j].Name)
	})
	return filtered, nil
}

func (s *Service) activeCandidates(snap Snapshot) []Candidate {
	paths := map[string]string{}
	for _, p := range snap.Panes {
		path := p.CWD
		if p.ForegroundCWD != "" {
			path = p.ForegroundCWD
		}
		if paths[p.WorkspaceID] == "" {
			paths[p.WorkspaceID] = path
		}
	}
	result := make([]Candidate, 0, len(snap.Workspaces))
	for _, w := range snap.Workspaces {
		c := Candidate{Kind: "herdr", Name: w.Label, Path: paths[w.ID], WorkspaceID: w.ID}
		s.enrich(&c)
		result = append(result, c)
	}
	return result
}

func (s *Service) configCandidates() ([]Candidate, error) {
	result := make([]Candidate, 0, len(s.cfg.Sessions))
	for _, sc := range s.cfg.Sessions {
		p, err := expandPath(sc.Path)
		if err != nil {
			return nil, err
		}
		result = append(result, Candidate{Kind: "config", Name: sc.Name, Path: p, Alias: sc.Alias, Icon: sc.Icon, Tabs: sc.Tabs, StartupCommand: sc.StartupCommand, PreviewCommand: sc.PreviewCommand, DisableStartup: sc.DisableStartupCommand})
	}
	return result, nil
}

func (s *Service) enrich(c *Candidate) {
	for _, sc := range s.cfg.Sessions {
		p, _ := expandPath(sc.Path)
		if strings.EqualFold(sc.Name, c.Name) || samePath(p, c.Path) {
			if c.Name == "" {
				c.Name = sc.Name
			}
			c.Alias = sc.Alias
			if sc.Icon != "" {
				c.Icon = sc.Icon
			}
			c.Tabs = sc.Tabs
			c.StartupCommand = sc.StartupCommand
			c.PreviewCommand = sc.PreviewCommand
			c.DisableStartup = sc.DisableStartupCommand
			return
		}
	}
	if c.Name == "" {
		c.Name = s.smartName(c.Path)
	}
	if wc, ok := s.matchWildcard(c.Path); ok {
		if wc.Icon != "" {
			c.Icon = wc.Icon
		}
		c.Tabs = wc.Tabs
		c.StartupCommand = wc.StartupCommand
		c.PreviewCommand = wc.PreviewCommand
		c.DisableStartup = wc.DisableStartupCommand
	}
}

func (s *Service) smartName(path string) string {
	if project, ok := s.projectForPath(path); ok {
		return projectDirectoryName(project, path)
	}
	return directoryName(path, s.cfg.DirLength)
}

func (s *Service) projectForPath(path string) (gitProject, bool) {
	if path == "" {
		return gitProject{}, false
	}
	if s.projectCache == nil {
		s.projectCache = map[string]projectCacheEntry{}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return gitProject{}, false
	}
	abs = filepath.Clean(abs)
	if cached, ok := s.projectCache[abs]; ok {
		return cached.project, cached.found
	}
	root, gitDir, commonDir, found := findGitRootFromFilesystem(abs)
	var project gitProject
	if found {
		rootKey := canonicalPath(root)
		if cached, ok := s.projectCache[rootKey]; ok && cached.found {
			project = cached.project
		} else {
			project = gitProjectFromMetadata(root, gitDir, commonDir, s.cfg)
		}
	}
	entry := projectCacheEntry{project: project, found: found}
	s.projectCache[abs] = entry
	if found {
		// Reuse metadata when naming another directory in the same repository.
		s.projectCache[fastPathKey(project.Root)] = entry
	}
	return project, found
}

func (s *Service) matchWildcard(path string) (WildcardConfig, bool) {
	for _, wc := range s.cfg.Wildcards {
		pattern, _ := expandPath(wc.Pattern)
		if strings.HasSuffix(pattern, "/**") {
			prefix := strings.TrimSuffix(pattern, "/**")
			if samePath(path, prefix) || strings.HasPrefix(filepath.Clean(path), filepath.Clean(prefix)+string(filepath.Separator)) {
				return wc, true
			}
		} else if ok, _ := filepath.Match(pattern, path); ok {
			return wc, true
		}
	}
	return WildcardConfig{}, false
}

func (s *Service) Resolve(value string) (Candidate, error) {
	value = strings.TrimSpace(value)
	path, err := expandPath(value)
	if err != nil {
		return Candidate{}, err
	}
	// Explicit paths bypass listing every workspace and zoxide directory.
	if strings.ContainsAny(value, `/\\`) || value == "." || value == ".." || value == "~" {
		if st, e := os.Stat(path); e == nil && st.IsDir() {
			c := Candidate{Kind: "zoxide", Path: path, Name: shortPath(path)}
			s.enrich(&c)
			return c, nil
		}
	}
	if s.client != nil && (s.client.socketPath != "" || s.client.dial != nil) {
		if snap, e := s.client.Snapshot(); e == nil {
			for _, c := range s.activeCandidates(snap) {
				if strings.EqualFold(value, c.Alias) || strings.EqualFold(value, c.Name) || value == c.WorkspaceID {
					return c, nil
				}
			}
		}
	}
	configured, err := s.configCandidates()
	if err != nil {
		return Candidate{}, err
	}
	for _, c := range configured {
		if strings.EqualFold(value, c.Alias) || strings.EqualFold(value, c.Name) {
			return c, nil
		}
	}
	if st, e := os.Stat(path); e == nil && st.IsDir() {
		c := Candidate{Kind: "zoxide", Path: path, Name: shortPath(path)}
		s.enrich(&c)
		return c, nil
	}
	if path, e := queryFrecency(s.cfg.Frecency, value); e == nil && path != "" {
		c := Candidate{Kind: "zoxide", Path: path, Name: shortPath(path)}
		s.enrich(&c)
		return c, nil
	}
	return Candidate{}, fmt.Errorf("no workspace or directory matches %q", value)
}

func (s *Service) Connect(c Candidate, command string) (Workspace, error) {
	snap, err := s.client.Snapshot()
	if err != nil {
		return Workspace{}, err
	}
	for _, w := range snap.Workspaces {
		// A directory's display name is not workspace identity. In particular,
		// separate checkouts and monorepo subdirectories must stay selectable.
		match := c.WorkspaceID != "" && w.ID == c.WorkspaceID
		if c.WorkspaceID == "" {
			if c.Kind == "config" {
				// A named session owns its startup commands and tabs. Another
				// workspace merely visiting its directory is not that session.
				match = strings.EqualFold(w.Label, c.Name)
			} else {
				match = workspaceHasPath(snap, w.ID, c.Path)
			}
		}
		if match {
			recordTransition(snap.FocusedWorkspaceID, w.ID)
			if err := s.client.WorkspaceFocus(w.ID); err != nil {
				return Workspace{}, err
			}
			return w, nil
		}
	}
	if st, err := os.Stat(c.Path); err != nil || !st.IsDir() {
		return Workspace{}, fmt.Errorf("workspace directory does not exist: %s", c.Path)
	}
	name := c.Name
	if c.Kind == "zoxide" || c.Kind == "find" {
		name = s.smartName(c.Path)
	}
	w, tab, pane, err := s.client.WorkspaceCreate(c.Path, name, true)
	if err != nil {
		return Workspace{}, err
	}
	recordTransition(snap.FocusedWorkspaceID, w.ID)
	tabs := c.Tabs
	if len(tabs) == 0 {
		tabs = s.cfg.DefaultSession.Tabs
	}
	startup := command
	if startup == "" && !c.DisableStartup {
		startup = c.StartupCommand
		if startup == "" {
			startup = s.cfg.DefaultSession.StartupCommand
		}
	}
	if len(tabs) > 0 {
		if err := s.applyTabs(w, tab, pane, tabs, startup); err != nil {
			return w, err
		}
	} else if startup != "" {
		if err := s.client.PaneRun(pane.ID, replaceBraces(startup, c.Path)); err != nil {
			return w, err
		}
	}
	addFrecency(s.cfg.Frecency, c.Path)
	return w, nil
}

func (s *Service) applyTabs(w Workspace, rootTab Tab, rootPane Pane, names []string, sessionStartup string) error {
	defs := map[string]TabConfig{}
	for _, v := range s.cfg.Tabs {
		defs[v.Name] = v
	}
	for i, name := range names {
		def := defs[name]
		path := wPath(rootPane.CWD, def.Path)
		var tab Tab
		var pane Pane
		var err error
		if i == 0 {
			tab = rootTab
			pane = rootPane
			if err = s.client.TabRename(tab.ID, name); err != nil {
				return err
			}
		} else {
			tab, pane, err = s.client.TabCreate(w.ID, path, name, false)
			if err != nil {
				return err
			}
		}
		startup := def.StartupCommand
		if i == 0 && sessionStartup != "" {
			// The selected session is more specific than a reusable tab
			// definition, so its startup command owns the root tab.
			startup = sessionStartup
		}
		if i == 0 && !samePath(path, rootPane.CWD) {
			changeDir := "cd " + shellQuote(path)
			if startup == "" {
				startup = changeDir
			} else {
				startup = changeDir + " && " + startup
			}
		}
		if startup != "" {
			if err = s.client.PaneRun(pane.ID, replaceBraces(startup, path)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) Preview(c Candidate) (string, error) {
	if c.WorkspaceID != "" {
		text, err := s.workspacePreview(c)
		if text != "" || (err != nil && c.Path == "") {
			return text, err
		}
	}
	cmd := c.PreviewCommand
	if cmd == "" {
		cmd = s.cfg.DefaultSession.PreviewCommand
	}
	if cmd != "" {
		command := shellCommand(replaceBraces(cmd, c.Path))
		command.Dir = c.Path
		out, err := command.CombinedOutput()
		if err != nil {
			return string(out), err
		}
		return string(out), nil
	}
	return directoryPreview(c.Path)
}

func replaceBraces(command, path string) string {
	return strings.ReplaceAll(command, "{}", shellQuote(path))
}
func wPath(root, value string) string {
	if value == "" {
		return root
	}
	p, _ := expandPath(value)
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	return p
}
func workspaceHasPath(s Snapshot, id, path string) bool {
	if path == "" {
		return false
	}
	for _, p := range s.Panes {
		if p.WorkspaceID == id && (samePath(p.CWD, path) || samePath(p.ForegroundCWD, path)) {
			return true
		}
	}
	return false
}
func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	aa, _ := filepath.Abs(a)
	bb, _ := filepath.Abs(b)
	return filepath.Clean(aa) == filepath.Clean(bb)
}
func isBlacklisted(c Candidate, list []string) bool {
	for _, v := range list {
		if strings.EqualFold(v, c.Name) || samePath(v, c.Path) {
			return true
		}
	}
	return false
}
func (s *Service) dedupeCandidates(in []Candidate) []Candidate {
	seenPaths := map[string]bool{}
	liveNames := map[string]bool{}
	seenWorkspaces := map[string]bool{}
	out := make([]Candidate, 0, len(in))
	for _, c := range in {
		pathKey := fastPathKey(c.Path)
		nameKey := strings.ToLower(strings.TrimSpace(c.Name))
		switch c.Kind {
		case "herdr":
			if c.WorkspaceID != "" && seenWorkspaces[c.WorkspaceID] {
				continue
			}
			seenWorkspaces[c.WorkspaceID] = true
			liveNames[nameKey] = true
		case "config":
			if liveNames[nameKey] {
				continue
			}
		default:
			if pathKey != "" && seenPaths[pathKey] {
				continue
			}
		}
		if pathKey != "" {
			seenPaths[pathKey] = true
		}
		out = append(out, c)
	}
	return out
}

func canonicalPath(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return filepath.Clean(abs)
}

func fastPathKey(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}
