package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

func (s *Service) workspacePreview(c Candidate) (string, error) {
	snap, err := s.client.Snapshot()
	if err != nil {
		return "", err
	}
	activeTab := ""
	for _, w := range snap.Workspaces {
		if w.ID == c.WorkspaceID {
			activeTab = w.ActiveTabID
			break
		}
	}
	var panes []Pane
	for _, p := range snap.Panes {
		if p.WorkspaceID == c.WorkspaceID && p.ID != os.Getenv("HERDR_SESH_PICKER_PANE") {
			// Exclude the picker itself to avoid capturing its own preview.
			panes = append(panes, p)
		}
	}
	sort.SliceStable(panes, func(i, j int) bool {
		if panes[i].Focused != panes[j].Focused {
			return panes[i].Focused
		}
		return panes[i].TabID == activeTab && panes[j].TabID != activeTab
	})
	parts := make([]string, len(panes))
	errs := make([]error, len(panes))
	// Overlap socket round trips without flooding Herdr for large workspaces.
	jobs := make(chan int, len(panes))
	for i := range panes {
		jobs <- i
	}
	close(jobs)
	var workers sync.WaitGroup
	for worker := 0; worker < min(4, len(panes)); worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				p := panes[i]
				text, err := s.client.PaneRead(p.ID, 80)
				errs[i] = err
				if err == nil && strings.TrimSpace(text) != "" {
					if len(panes) > 1 {
						text = fmt.Sprintf("── %s · %s ──\n%s", p.ID, shortPath(p.CWD), text)
					}
					parts[i] = text
				}
			}
		}()
	}
	workers.Wait()
	var rendered []string
	for i, part := range parts {
		if part != "" {
			rendered = append(rendered, part)
		}
		if errs[i] != nil {
			err = errs[i]
		}
	}
	if len(rendered) > 0 {
		return strings.Join(rendered, "\n\n"), nil
	}
	return "", err
}

func directoryPreview(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	for _, tool := range [][]string{{"eza", "--oneline", "--all", "--group-directories-first", "--color=always", "--icons=auto", "--", path}, {"lsd", "-la", "--oneline", "--color=always", "--", path}, {"tree", "-a", "-L", "2", "-C", path}} {
		if p, err := exec.LookPath(tool[0]); err == nil {
			cmd := exec.Command(p, tool[1:]...)
			out, e := cmd.CombinedOutput()
			if e == nil {
				return string(out), nil
			}
		}
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", path)
	for _, e := range entries {
		suffix := ""
		if e.IsDir() {
			suffix = string(filepath.Separator)
		}
		fmt.Fprintf(&b, "%s%s\n", e.Name(), suffix)
	}
	return b.String(), nil
}
