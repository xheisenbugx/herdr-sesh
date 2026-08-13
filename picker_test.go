package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestPickerSearchIncludesDisplayedWorkspaceName(t *testing.T) {
	if _, err := exec.LookPath("fzf"); err != nil {
		t.Skip("fzf is not installed")
	}

	rows := "encoded\tbackend\t/Users/osvaldo\n" +
		"encoded2\tpulsar-backend-kb\t/Users/osvaldo/Workspace/pulsar-backend-kb\n"
	args := append(pickerFieldArgs(), "--filter=backe")
	cmd := exec.Command("fzf", args...)
	cmd.Stdin = strings.NewReader(rows)
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "encoded\tbackend\t/Users/osvaldo") {
		t.Fatalf("displayed workspace name was not searchable; output: %q", output)
	}
}
