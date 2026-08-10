package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func shellCommand(command string) *exec.Cmd { return exec.Command("/bin/sh", "-lc", command) }

func replacePlaceholder(command, value string) string {
	return strings.ReplaceAll(command, "{}", shellQuote(value))
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

func listFrecency(cfg FrecencyConfig) ([]Candidate, error) {
	cmd := frecencyCommand(cfg.ListCommand, "")
	out, err := cmd.Output()
	if err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) {
			return nil, nil
		}
		return nil, fmt.Errorf("frecency list command: %w", err)
	}
	var result []Candidate
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		path, score := line, 0.0
		fields := strings.Fields(line)
		if len(fields) > 1 {
			if n, e := strconv.ParseFloat(fields[0], 64); e == nil {
				score = n
				path = strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
			}
		}
		path, err = expandPath(path)
		if err != nil {
			continue
		}
		result = append(result, Candidate{Kind: "zoxide", Path: path, Score: score})
	}
	return result, scanner.Err()
}

func queryFrecency(cfg FrecencyConfig, query string) (string, error) {
	out, err := frecencyCommand(cfg.QueryCommand, query).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func addFrecency(cfg FrecencyConfig, path string) {
	cmd := frecencyCommand(cfg.AddCommand, path)
	if cmd.Start() == nil {
		go func() { _ = cmd.Wait() }()
	}
}

// frecencyCommand runs ordinary argv-style commands directly. Avoiding a login
// shell saves roughly 100ms on macOS for the default zoxide query. Commands that
// intentionally use shell operators still fall back to /bin/sh -lc.
func frecencyCommand(command, value string) *exec.Cmd {
	if !requiresShell(command) {
		if argv, err := splitCommandLine(command); err == nil && len(argv) > 0 {
			for i := range argv {
				argv[i] = strings.ReplaceAll(argv[i], "{}", value)
			}
			return exec.Command(argv[0], argv[1:]...)
		}
	}
	return shellCommand(replacePlaceholder(command, value))
}

func requiresShell(command string) bool {
	quote := rune(0)
	escaped := false
	for _, r := range command {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if strings.ContainsRune("|&;<>()$`\n", r) {
			return true
		}
	}
	return false
}

func splitCommandLine(command string) ([]string, error) {
	var args []string
	var current strings.Builder
	quote := rune(0)
	escaped := false
	hasValue := false
	flush := func() {
		if hasValue {
			args = append(args, current.String())
			current.Reset()
			hasValue = false
		}
	}
	for _, r := range command {
		if escaped {
			current.WriteRune(r)
			hasValue = true
			escaped = false
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
				hasValue = true
			}
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
			hasValue = true
		case ' ', '\t', '\r', '\n':
			flush()
		default:
			current.WriteRune(r)
			hasValue = true
		}
	}
	if escaped || quote != 0 {
		return nil, fmt.Errorf("unterminated quote or escape in command")
	}
	flush()
	return args, nil
}
