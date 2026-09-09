package main_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCIWorkflowSupportsStackedPullRequests(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	on, err := parseWorkflowOn(data)
	if err != nil {
		t.Fatal(err)
	}
	prRaw, ok := on["pull_request"]
	if !ok {
		t.Fatal("CI must run on pull_request, including stacked PRs")
	}
	if prRaw == nil {
		return
	}
	pr, err := asStringKeyMap(prRaw)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"branches", "branches-ignore"} {
		if _, exists := pr[key]; exists {
			t.Errorf("CI pull_request must not set %s: stacked PRs may target any branch", key)
		}
	}
}

func TestCIWorkflowUsesReadOnlyUnpersistedCredentials(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var wf struct {
		Permissions map[string]string `yaml:"permissions"`
		Jobs        map[string]struct {
			Permissions map[string]string `yaml:"permissions"`
			Steps       []struct {
				Uses string         `yaml:"uses"`
				With map[string]any `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &wf); err != nil {
		t.Fatal(err)
	}
	if len(wf.Permissions) != 1 || wf.Permissions["contents"] != "read" {
		t.Errorf("CI permissions = %v, want only contents: read", wf.Permissions)
	}
	var checkouts int
	for name, job := range wf.Jobs {
		for scope, access := range job.Permissions {
			if access != "none" && (scope != "contents" || access != "read") {
				t.Errorf("job %s broadens CI permissions: %s: %s", name, scope, access)
			}
		}
		for _, step := range job.Steps {
			if !strings.HasPrefix(step.Uses, "actions/checkout@") {
				continue
			}
			checkouts++
			if step.With["persist-credentials"] != false {
				t.Errorf("job %s checkout must set persist-credentials: false", name)
			}
		}
	}
	if checkouts == 0 {
		t.Fatal("CI has no checkout steps")
	}
}
