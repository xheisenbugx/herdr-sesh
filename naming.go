package main

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var unsafeLabel = regexp.MustCompile(`[\x00-\x1f\x7f]`)

type gitProject struct {
	Root string
	Name string
}

// smartName deliberately discovers Git metadata through the filesystem instead
// of spawning git. The picker may name hundreds of zoxide paths at startup, and
// two git processes per path made opening it noticeably slow.
func smartName(path string, cfg Config) string {
	if project, ok := findGitProject(path, cfg); ok {
		return sanitizeName(project.Name)
	}
	return directoryName(path, cfg.DirLength)
}

func directoryName(path string, length int) string {
	clean := filepath.Clean(path)
	parts := strings.Split(clean, string(filepath.Separator))
	if length < 1 {
		length = 1
	}
	if len(parts) < length {
		length = len(parts)
	}
	return sanitizeName(strings.Join(parts[len(parts)-length:], "/"))
}

func findGitProject(path string, cfg Config) (gitProject, bool) {
	root, gitDir, commonDir, ok := findGitRootFromFilesystem(path)
	if !ok {
		return gitProject{}, false
	}
	return gitProjectFromMetadata(root, gitDir, commonDir, cfg), true
}

func gitProjectFromMetadata(root, gitDir, commonDir string, cfg Config) gitProject {
	if cfg.GitUseWorktreeRoot && commonDir != "" && filepath.Base(commonDir) == ".git" {
		root = filepath.Dir(commonDir)
	}
	name := ""
	configDir := gitDir
	if commonDir != "" {
		configDir = commonDir
	}
	if remote := originRemote(filepath.Join(configDir, "config")); remote != "" {
		name = parseRepoName(remote)
	}
	if name == "" {
		name = filepath.Base(root)
	}
	if cfg.GitDirLength > 1 {
		parent := filepath.Dir(root)
		extras := []string{}
		for i := 1; i < cfg.GitDirLength; i++ {
			base := filepath.Base(parent)
			if base == "." || base == string(filepath.Separator) {
				break
			}
			extras = append([]string{base}, extras...)
			parent = filepath.Dir(parent)
		}
		if len(extras) > 0 {
			name = strings.Join(append(extras, name), "/")
		}
	}
	return gitProject{Root: filepath.Clean(root), Name: name}
}

func findGitRootFromFilesystem(path string) (root, gitDir, commonDir string, found bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", "", false
	}
	if root, gitDir, commonDir, found = findGitRootAt(abs); found {
		return root, gitDir, commonDir, true
	}
	// Symlink resolution is the uncommon fallback. Keeping it out of the normal
	// zoxide path avoids hundreds of filesystem allocations on every picker open.
	if resolved, err := filepath.EvalSymlinks(abs); err == nil && resolved != abs {
		return findGitRootAt(resolved)
	}
	return "", "", "", false
}

func findGitRootAt(path string) (root, gitDir, commonDir string, found bool) {
	abs := path
	if info, err := os.Stat(abs); err == nil && !info.IsDir() {
		abs = filepath.Dir(abs)
	}
	for dir := filepath.Clean(abs); ; dir = filepath.Dir(dir) {
		dotGit := filepath.Join(dir, ".git")
		if info, err := os.Stat(dotGit); err == nil {
			if info.IsDir() {
				return dir, dotGit, dotGit, true
			}
			if target := gitDirFileTarget(dotGit); target != "" {
				common := commonGitDir(target)
				return dir, target, common, true
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return "", "", "", false
}

func gitDirFileTarget(dotGit string) string {
	data, err := os.ReadFile(dotGit)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	const prefix = "gitdir:"
	if !strings.HasPrefix(strings.ToLower(line), prefix) {
		return ""
	}
	target := strings.TrimSpace(line[len(prefix):])
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(dotGit), target)
	}
	return filepath.Clean(target)
}

func commonGitDir(gitDir string) string {
	data, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if err != nil {
		return gitDir
	}
	common := strings.TrimSpace(string(data))
	if !filepath.IsAbs(common) {
		common = filepath.Join(gitDir, common)
	}
	return filepath.Clean(common)
}

func originRemote(configPath string) string {
	file, err := os.Open(configPath)
	if err != nil {
		return ""
	}
	defer file.Close()
	inOrigin := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			inOrigin = strings.EqualFold(line, `[remote "origin"]`)
			continue
		}
		if !inOrigin {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok && strings.EqualFold(strings.TrimSpace(key), "url") {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func parseRepoName(remote string) string {
	remote = strings.TrimSuffix(strings.TrimSpace(remote), ".git")
	if u, err := url.Parse(remote); err == nil && u.Path != "" {
		return filepath.Base(u.Path)
	}
	if i := strings.LastIndex(remote, ":"); i >= 0 {
		remote = remote[i+1:]
	}
	return filepath.Base(remote)
}

func gitRoot(path string) (string, error) {
	if project, ok := findGitProject(path, Config{}); ok {
		return project.Root, nil
	}
	// Fall back for unusual Git layouts such as bare repositories.
	out, err := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", fmt.Errorf("git returned an empty root")
	}
	return root, nil
}

func sanitizeName(name string) string {
	return strings.TrimSpace(unsafeLabel.ReplaceAllString(name, ""))
}
