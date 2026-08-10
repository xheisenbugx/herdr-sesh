package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func directoryPreview(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	for _, tool := range [][]string{{"eza", "--all", "--group-directories-first", "--color=always", "--icons=auto", "--", path}, {"lsd", "-la", "--color=always", "--", path}, {"tree", "-a", "-L", "2", "-C", path}} {
		if p, err := exec.LookPath(tool[0]); err == nil {
			cmd := exec.Command(p, tool[1:]...)
			out, e := cmd.CombinedOutput()
			return string(out), e
		}
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
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
