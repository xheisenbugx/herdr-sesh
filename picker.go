package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func encodeCandidate(c Candidate) string {
	data, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(data)
}
func decodeCandidate(value string) (Candidate, error) {
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return Candidate{}, err
	}
	var c Candidate
	err = json.Unmarshal(data, &c)
	return c, err
}

func pickerLines(service *Service, kinds map[string]bool, writer io.Writer) error {
	list, err := service.List(kinds, false)
	if err != nil {
		return err
	}
	show := service.cfg.TUI.ShowIcons == nil || *service.cfg.TUI.ShowIcons
	for _, c := range list {
		fmt.Fprintf(writer, "%s\t%s\t%s\n", encodeCandidate(c), c.displayName(show), c.Path)
	}
	return nil
}

func runPickerUI(service *Service, query string) error {
	if _, err := exec.LookPath("fzf"); err != nil {
		return errors.New("fzf is required for the interactive picker; install fzf or use `herdr-sesh list` + `herdr-sesh connect`")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	var input bytes.Buffer
	if err := pickerLines(service, map[string]bool{}, &input); err != nil {
		return err
	}
	preview := shellQuote(exe) + " preview --encoded {1}"
	reload := func(kind string) string { return shellQuote(exe) + " list --picker-lines --source " + kind }
	args := []string{"--ansi", "--delimiter=\t", "--with-nth=2..", "--nth=2..", "--prompt=" + service.cfg.TUI.Prompt, "--header=" + service.cfg.TUI.Header, "--bind=tab:down,btab:up", "--bind=ctrl-a:reload(" + reload("all") + ")", "--bind=ctrl-w:reload(" + reload("herdr") + ")", "--bind=ctrl-g:reload(" + reload("config") + ")", "--bind=ctrl-z:reload(" + reload("zoxide") + ")", "--bind=ctrl-d:execute(" + shellQuote(exe) + " close --encoded {1})+reload(" + reload("all") + ")"}
	if query != "" {
		args = append(args, "--query="+query)
	}
	if service.cfg.TUI.Reverse == nil || *service.cfg.TUI.Reverse {
		args = append(args, "--layout=reverse")
	}
	if service.cfg.TUI.Preview == nil || *service.cfg.TUI.Preview {
		args = append(args, "--preview="+preview, "--preview-window=right:"+strconv.Itoa(service.cfg.TUI.PreviewWidth)+"%")
	}
	cmd := exec.Command("fzf", args...)
	cmd.Stdin = &input
	cmd.Stderr = os.Stderr
	var selected bytes.Buffer
	cmd.Stdout = &selected
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && (exit.ExitCode() == 1 || exit.ExitCode() == 130) {
			return nil
		}
		return err
	}
	fields := strings.Split(strings.TrimSpace(selected.String()), "\t")
	if len(fields) == 0 || fields[0] == "" {
		return nil
	}
	candidate, err := decodeCandidate(fields[0])
	if err != nil {
		return err
	}
	_, err = service.Connect(candidate, "")
	return err
}
