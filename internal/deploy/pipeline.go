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

	// 4.5. Perform Blue-Green Health Check if configured
	if err := s.performHealthCheck(ctx, p, env, containerID, d.ID); err != nil {
		return err
	}

	// 5. Transition to Running
	if err := s.Transition(ctx, d.ID, models.DeploymentStatusRunning, nil); err != nil {
		return err
	}

	// 6. Cleanup old deployments (async)
	//nolint:contextcheck // cleanup is background task
	go func(envID, deployID string) {
		// Create a new context for cleanup as the request context might be canceled
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
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

// performHealthCheck executes blue-green health checks if configured for the project.
func (s *Service) performHealthCheck(ctx context.Context, p *models.Project, env *models.Environment, containerID, deploymentID string) error {
	if p.HealthCheckPath == "" || s.blueGreenDeployer == nil {
		return nil
	}

	containerInfo, inspectErr := s.dockerClient.InspectContainer(ctx, containerID)
	if inspectErr != nil {
		s.logger.Warn("failed to inspect container for health check", "container_id", containerID, "error", inspectErr)
		return nil // Continue deployment without health check if inspection fails
	}

	ipAddress := ""
	if network, ok := containerInfo.NetworkSettings.Networks[s.traefikNetwork]; ok {
		ipAddress = network.IPAddress
	}

	if ipAddress == "" {
		return nil // Continue deployment if no IP found
	}

	port := "80"
	if env.Port.Valid {
		port = fmt.Sprintf("%d", env.Port.Int32)
	}

	healthCheckURL := fmt.Sprintf("http://%s:%s", ipAddress, port)

	req := BlueGreenRequest{
		DeploymentID:    deploymentID,
		NewContainerID:  containerID,
		OldContainerID:  "", // Let CleanupOldDeployments handle DB-aware cleanup
		HealthCheckURL:  healthCheckURL,
		HealthCheckPath: p.HealthCheckPath,
	}

	_, bgErr := s.blueGreenDeployer.Deploy(ctx, req)
	if bgErr != nil {
		// Rollback container
		s.logger.Error("health check failed, rolling back", "deployment_id", deploymentID, "error", bgErr)
		if stopErr := s.dockerClient.StopContainer(ctx, containerID); stopErr != nil {
			s.logger.Error("failed to stop container during rollback", "container_id", containerID, "error", stopErr)
		}
		// Update container status in DB
		containers, err := s.containerRepo.ListByDeploymentID(ctx, deploymentID)
		if err != nil {
			s.logger.Warn("failed to list containers during rollback", "deployment_id", deploymentID, "error", err)
		} else {
			for _, c := range containers {
				if updateErr := s.containerRepo.UpdateStatus(ctx, c.ID, models.ContainerStatusExited); updateErr != nil {
					s.logger.Warn("failed to update container status during rollback", "container_id", c.ID, "error", updateErr)
				}
			}
		}
		return fmt.Errorf("health check failed: %w", bgErr)
	}

	return nil
}
