// Package deploy handles deployment orchestration.
package deploy

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/user/gocool/internal/docker"
	"github.com/user/gocool/internal/git"
	"github.com/user/gocool/internal/models"
	"github.com/user/gocool/internal/proxy"
)

// RunPipeline executes the full deployment pipeline.
func (s *Service) RunPipeline(ctx context.Context, deploymentID string) error {
	d, err := s.GetDeployment(ctx, deploymentID)
	if err != nil {
		return err
	}

	env, err := s.environmentRepo.GetByID(ctx, d.EnvironmentID)
	if err != nil {
		return fmt.Errorf("failed to get environment: %w", err)
	}

	p, err := s.projectRepo.GetByID(ctx, env.ProjectID)
	if err != nil {
		return fmt.Errorf("failed to get project: %w", err)
	}

	// 1. Transition to Cloning
	if tErr := s.Transition(ctx, d.ID, models.DeploymentStatusCloning, nil); tErr != nil {
		return tErr
	}

	workDir := filepath.Join(os.TempDir(), "gocool-builds", d.ID)
	if mErr := os.MkdirAll(workDir, 0o700); mErr != nil {
		return mErr
	}
	defer os.RemoveAll(workDir)

	branch := p.DefaultBranch
	if env.Branch.Valid {
		branch = env.Branch.String
	}

	if cErr := s.gitService.Clone(ctx, git.CloneOptions{
		URL:    p.RepositoryURL,
		Branch: branch,
		Path:   workDir,
		Depth:  1,
	}); cErr != nil {
		return fmt.Errorf("git clone failed: %w", cErr)
	}

	// 2. Transition to Building
	if tErr := s.Transition(ctx, d.ID, models.DeploymentStatusBuilding, nil); tErr != nil {
		return tErr
	}

	imageName := fmt.Sprintf("gocool-%s:%s", p.ID, d.ID)
	if bErr := s.dockerClient.BuildImage(ctx, docker.BuildOptions{
		ImageName:  imageName,
		Dockerfile: p.DockerfilePath,
		ContextDir: workDir,
	}, os.Stdout); bErr != nil {
		return fmt.Errorf("docker build failed: %w", bErr)
	}

	// 3. Transition to Deploying
	if tErr := s.Transition(ctx, d.ID, models.DeploymentStatusDeploying, nil); tErr != nil {
		return tErr
	}

	// Generate Traefik labels
	labels := proxy.GenerateLabels(p, env, "traefik")

	containerName := fmt.Sprintf("gocool-%s-%s-%s", p.ID, env.Name, d.ID)
	containerID, err := s.dockerClient.CreateContainer(ctx, docker.ContainerOptions{
		Name:   containerName,
		Image:  imageName,
		Labels: labels,
	})
	if err != nil {
		return fmt.Errorf("docker deploy failed: %w", err)
	}

	// 4. Record container in DB
	if err := s.containerRepo.Create(ctx, &models.Container{
		DeploymentID: sql.NullString{String: d.ID, Valid: true},
		DockerID:     containerID,
		Name:         containerName,
		Image:        imageName,
		Status:       models.ContainerStatusRunning,
	}); err != nil {
		// Log but don't fail deployment record
		fmt.Printf("failed to record container in DB: %v\n", err)
	}

	// 5. Transition to Running
	return s.Transition(ctx, d.ID, models.DeploymentStatusRunning, nil)
}
