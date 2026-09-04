package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const composeCommandTimeout = 2 * time.Minute

var composeFiles = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

// composeController limits Compose actions to projects directly inside sourcesDir.
// Keeping the allowed root explicit prevents a request from using an arbitrary
// working directory on the host.
type composeController struct {
	sourcesDir string
}

func newComposeController(sourcesDir string) *composeController {
	if sourcesDir == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			sourcesDir = filepath.Join(home, "sources")
		}
	}
	if sourcesDir == "" {
		sourcesDir = "sources"
	}
	return &composeController{sourcesDir: filepath.Clean(sourcesDir)}
}

// projects returns only immediate child folders that contain a Compose file.
func (cc *composeController) projects() ([]string, error) {
	entries, err := os.ReadDir(cc.sourcesDir)
	if err != nil {
		return nil, fmt.Errorf("read sources directory: %w", err)
	}

	projects := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if hasComposeFile(filepath.Join(cc.sourcesDir, entry.Name())) {
			projects = append(projects, entry.Name())
		}
	}
	return projects, nil
}

func hasComposeFile(dir string) bool {
	for _, name := range composeFiles {
		info, err := os.Stat(filepath.Join(dir, name))
		if err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

// projectDir validates a selected project name and resolves it inside sourcesDir.
func (cc *composeController) projectDir(project string) (string, error) {
	if project == "" || filepath.IsAbs(project) || filepath.Base(project) != project {
		return "", errors.New("select a project from the list")
	}

	root, err := filepath.EvalSymlinks(cc.sourcesDir)
	if err != nil {
		return "", fmt.Errorf("resolve sources directory: %w", err)
	}
	dir, err := filepath.EvalSymlinks(filepath.Join(root, project))
	if err != nil {
		return "", errors.New("selected project no longer exists")
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("selected project is outside the sources directory")
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() || !hasComposeFile(dir) {
		return "", errors.New("selected folder does not contain a Compose file")
	}
	return dir, nil
}

func (cc *composeController) run(ctx context.Context, project, action string) (string, error) {
	dir, err := cc.projectDir(project)
	if err != nil {
		return "", err
	}
	if action != "up" && action != "down" {
		return "", errors.New("action must be up or down")
	}

	args := []string{"compose", action}
	if action == "up" {
		args = append(args, "-d")
	}
	commandCtx, cancel := context.WithTimeout(ctx, composeCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, "docker", args...)
	cmd.Dir = dir
	output, runErr := cmd.CombinedOutput()
	if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
		return string(output), fmt.Errorf("docker compose %s timed out", action)
	}
	if runErr != nil {
		return string(output), fmt.Errorf("docker compose %s: %w", action, runErr)
	}
	return string(output), nil
}
