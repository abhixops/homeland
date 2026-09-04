package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestComposeProjectsAndProjectDir(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "paperless")
	if err := os.Mkdir(project, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "compose.yaml"), []byte("services: {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "not-compose"), 0755); err != nil {
		t.Fatal(err)
	}

	controller := newComposeController(root)
	projects, err := controller.projects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0] != "paperless" {
		t.Fatalf("projects = %v; want [paperless]", projects)
	}
	got, err := controller.projectDir("paperless")
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("project dir = %q; want %q", got, want)
	}
}

func TestComposeProjectDirRejectsOutsideSources(t *testing.T) {
	root := t.TempDir()
	controller := newComposeController(root)
	for _, project := range []string{"", "../outside", "/tmp/outside", "nested/project"} {
		if _, err := controller.projectDir(project); err == nil {
			t.Errorf("projectDir(%q) succeeded; want error", project)
		}
	}
}
