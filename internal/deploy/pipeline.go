// Package deploy handles deployment orchestration.
package deploy

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/git"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/proxy"
	"github.com/raythurman2386/goforge/internal/services"
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

	// Fetch git source for authentication if project has one
	var accessToken string
	if p.GitSourceID.Valid {
		gitSource, gsErr := s.gitSourceRepo.GetByID(ctx, p.GitSourceID.String)
		if gsErr != nil {
			return fmt.Errorf("failed to get git source: %w", gsErr)
		}
		if gitSource.AccessToken.Valid {
			accessToken = gitSource.AccessToken.String
		}
	}

	// 1. Transition to Cloning
	if tErr := s.Transition(ctx, d.ID, models.DeploymentStatusCloning, nil); tErr != nil {
		return tErr
	}

	workDir := filepath.Join(s.buildDir, "goforge-builds", d.ID)
	if mErr := os.MkdirAll(workDir, 0o700); mErr != nil {
		return mErr
	}
	defer os.RemoveAll(workDir)

	branch := p.DefaultBranch
	if env.Branch.Valid {
		branch = env.Branch.String
	}

	if cErr := s.gitService.Clone(ctx, git.CloneOptions{
		URL:         p.RepositoryURL,
		Branch:      branch,
		Path:        workDir,
		Depth:       1,
		AccessToken: accessToken,
	}); cErr != nil {
		return fmt.Errorf("git clone failed: %w", cErr)
	}

	// 2. Transition to Building
	if tErr := s.Transition(ctx, d.ID, models.DeploymentStatusBuilding, nil); tErr != nil {
		return tErr
	}

	imageName := fmt.Sprintf("goforge-%s:%s", p.ID, d.ID)
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
	labels := proxy.GenerateLabelsForCloudflare(p, env, s.traefikNetwork, s.cloudflareProxyMode)

	// Collect environment variables for the container
	containerEnv := s.buildContainerEnv(ctx, d.EnvironmentID, env.ProjectID)

	containerName := fmt.Sprintf("goforge-%s-%s-%s", p.ID, env.Name, d.ID)
	containerID, err := s.dockerClient.CreateContainer(ctx, docker.ContainerOptions{
		Name:        containerName,
		Image:       imageName,
		Env:         containerEnv,
		Labels:      labels,
		NetworkName: s.traefikNetwork,
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
		s.logger.Error("failed to record container in DB", "error", err, "deployment_id", d.ID)
	}

	// 5. Transition to Running
	if err := s.Transition(ctx, d.ID, models.DeploymentStatusRunning, nil); err != nil {
		return err
	}

	// 6. Cleanup old deployments (async)
	//nolint:contextcheck // cleanup is background task
	go func(envID, deployID string) {
		// Create a new context for cleanup as the request context might be canceled
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := s.CleanupOldDeployments(cleanupCtx, envID, deployID); err != nil {
			s.logger.Error("failed to cleanup old deployments", "error", err, "environment_id", envID)
		}
	}(d.EnvironmentID, d.ID)

	return nil
}

// buildContainerEnv collects environment variables from user-defined env vars
// and linked service connection strings for injection into the deployment container.
func (s *Service) buildContainerEnv(ctx context.Context, environmentID, projectID string) []string {
	var env []string

	// 1. User-defined environment variables
	if s.envVariableRepo != nil {
		vars, err := s.envVariableRepo.ListByEnvironmentID(ctx, environmentID)
		if err != nil {
			s.logger.Warn("failed to fetch env variables for deployment", "environment_id", environmentID, "error", err)
		} else {
			for _, v := range vars {
				env = append(env, v.Key+"="+v.Value)
			}
		}
	}

	// 2. Linked service connection strings (conventional aliases: DATABASE_URL, REDIS_URL, etc.)
	if s.projectServiceInstanceRepo != nil {
		instances, err := s.projectServiceInstanceRepo.ListByProjectID(ctx, projectID)
		if err != nil {
			s.logger.Warn("failed to fetch linked services for deployment", "project_id", projectID, "error", err)
		} else {
			for _, inst := range instances {
				connInfo, ciErr := inst.GetConnectionInfo()
				if ciErr != nil {
					s.logger.Warn("failed to parse connection info", "instance_id", inst.ID, "error", ciErr)
					continue
				}
				for connKey, val := range connInfo {
					envKey := services.ServiceEnvVarName(inst.Name, inst.TemplateName, connKey)
					env = append(env, envKey+"="+val)
				}
			}
		}
	}

	return env
}
