package deploy

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const defaultDockerfile = "Dockerfile"

// BuildSpec is a Dockerfile and context inside a cloned repository.
// Paths are slash-separated and relative. They cannot escape the clone.
type BuildSpec struct {
	Dockerfile string
	Context    string
}

// ParseBuildSpec normalizes the project fields. An empty Dockerfile is
// "Dockerfile". An empty context is the repository root.
func ParseBuildSpec(dockerfile, contextDir string) (BuildSpec, error) {
	if strings.TrimSpace(dockerfile) == "" {
		dockerfile = defaultDockerfile
	}
	if strings.TrimSpace(contextDir) == "" {
		contextDir = "."
	}
	df, err := cleanRepoPath(dockerfile)
	if err != nil {
		return BuildSpec{}, fmt.Errorf("invalid dockerfile path: %w", err)
	}
	ctx, err := cleanRepoPath(contextDir)
	if err != nil {
		return BuildSpec{}, fmt.Errorf("invalid build context: %w", err)
	}
	if df == "." {
		return BuildSpec{}, fmt.Errorf("invalid dockerfile path: path must name a file")
	}
	return BuildSpec{Dockerfile: df, Context: ctx}, nil
}

// Locate checks that the context directory and Dockerfile exist under workDir.
// The returned Dockerfile path is relative to the context, which is what the
// Docker API expects.
func (s BuildSpec) Locate(workDir string) (contextDir, dockerfile string, err error) {
	root := filepath.Clean(workDir)
	contextDir = root
	if s.Context != "." {
		contextDir = filepath.Join(root, filepath.FromSlash(s.Context))
	}
	if !pathInside(root, contextDir) {
		return "", "", fmt.Errorf("build context escapes the repository")
	}
	info, statErr := os.Stat(contextDir)
	if statErr != nil || !info.IsDir() {
		return "", "", fmt.Errorf("build context not found: %s", s.Context)
	}

	filePath := filepath.Join(contextDir, filepath.FromSlash(s.Dockerfile))
	if !pathInside(contextDir, filePath) {
		return "", "", fmt.Errorf("dockerfile path escapes the build context")
	}
	fileInfo, fileErr := os.Stat(filePath)
	if fileErr != nil || fileInfo.IsDir() {
		return "", "", fmt.Errorf("dockerfile not found: %s", s.Dockerfile)
	}
	rel, relErr := filepath.Rel(contextDir, filePath)
	if relErr != nil {
		return "", "", fmt.Errorf("dockerfile not found: %s", s.Dockerfile)
	}
	return contextDir, filepath.ToSlash(rel), nil
}

func cleanRepoPath(raw string) (string, error) {
	if strings.ContainsRune(raw, 0) {
		return "", fmt.Errorf("path contains a null byte")
	}
	if strings.Contains(raw, "\\") {
		return "", fmt.Errorf("path must use slashes")
	}
	if path.IsAbs(raw) {
		return "", fmt.Errorf("path must be relative")
	}
	cleaned := path.Clean(raw)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("path escapes the repository")
	}
	return cleaned, nil
}

func pathInside(root, target string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
