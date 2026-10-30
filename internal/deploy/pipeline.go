// Package deploy handles deployment orchestration.
package deploy

import (
	"context"
	"database/sql"
	"errors"
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
// A non-nil error leaves the deployment in the failed status.
func (s *Service) RunPipeline(ctx context.Context, deploymentID string) (err error) {
	defer func() {
		err = s.finishPipeline(ctx, deploymentID, err)
	}()

	d, err := s.deploymentForRun(ctx, deploymentID)
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

	if domain := deploymentDomain(p, env); domain != "" && !env.Port.Valid {
		return fmt.Errorf("container port is required when a domain is set")
	}

	var accessToken string
	if p.GitSourceID.Valid && s.gitSourceRepo != nil {
		gitSource, gsErr := s.gitSourceRepo.GetByID(ctx, p.GitSourceID.String)
		if gsErr != nil {
			return fmt.Errorf("failed to get git source: %w", gsErr)
		}
		if gitSource.AccessToken.Valid {
			accessToken = gitSource.AccessToken.String
		}
	}

	workDir, err := s.cloneForDeployment(ctx, d, p, env, accessToken)
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	imageName, err := s.buildForDeployment(ctx, d, p, workDir)
	if err != nil {
		return err
	}

	if tErr := s.Transition(ctx, d.ID, models.DeploymentStatusDeploying, nil); tErr != nil {
		return tErr
	}
	s.recordLog(ctx, d.ID, "info", "deploy", "starting container")

	labels := proxy.GenerateLabelsForCloudflare(p, env, s.traefikNetwork, s.cloudflareProxyMode)
	containerEnv := s.buildContainerEnv(ctx, d.EnvironmentID, env.ProjectID)

	containerName := fmt.Sprintf("goforge-%s-%s-%s", p.ID, env.Name, d.ID)
	containerID, err := s.dockerClient.CreateContainer(ctx, docker.ContainerOptions{
		Name:          containerName,
		Image:         imageName,
		Env:           containerEnv,
		Labels:        labels,
		NetworkName:   s.traefikNetwork,
		RestartPolicy: docker.RestartUnlessStopped,
	})
	if err != nil {
		return fmt.Errorf("docker deploy failed: %w", err)
	}

	if s.containerRepo != nil {
		if cErr := s.containerRepo.Create(ctx, &models.Container{
			DeploymentID: sql.NullString{String: d.ID, Valid: true},
			DockerID:     containerID,
			Name:         containerName,
			Image:        imageName,
			Status:       models.ContainerStatusRunning,
		}); cErr != nil && s.logger != nil {
			s.logger.Error("failed to record container in DB", "error", cErr, "deployment_id", d.ID)
		}
	}

	if hErr := s.performHealthCheck(ctx, p, env, containerID, d.ID); hErr != nil {
		return hErr
	}

	if tErr := s.Transition(ctx, d.ID, models.DeploymentStatusRunning, nil); tErr != nil {
		return tErr
	}
	s.recordLog(ctx, d.ID, "info", "deploy", "deployment is running")

	//nolint:contextcheck // cleanup is a background task
	go func(envID, deployID string) {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if cErr := s.CleanupOldDeployments(cleanupCtx, envID, deployID); cErr != nil && s.logger != nil {
			s.logger.Error("failed to cleanup old deployments", "error", cErr, "environment_id", envID)
		}
	}(d.EnvironmentID, d.ID)

	return nil
}

func (s *Service) deploymentForRun(ctx context.Context, deploymentID string) (*models.Deployment, error) {
	d, err := s.GetDeployment(ctx, deploymentID)
	if err != nil {
		return nil, err
	}
	if d.Status == models.DeploymentStatusCanceled {
		return nil, ErrCanceled
	}
	return d, nil
}

func (s *Service) finishPipeline(ctx context.Context, deploymentID string, err error) error {
	if err == nil {
		return nil
	}
	if canceledDeployment(ctx, s, deploymentID, err) {
		return ErrCanceled
	}
	msg := err.Error()
	s.recordLog(ctx, deploymentID, "error", "pipeline", msg)
	if tErr := s.Transition(ctx, deploymentID, models.DeploymentStatusFailed, &msg); tErr != nil && s.logger != nil {
		s.logger.Error("failed to mark deployment failed", "deployment_id", deploymentID, "error", tErr)
	}
	return err
}

func canceledDeployment(ctx context.Context, s *Service, deploymentID string, err error) bool {
	if errors.Is(err, ErrCanceled) || errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return true
	}
	// The pipeline context may already be canceled, so read status with a fresh one.
	lookupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	current, getErr := s.GetDeployment(lookupCtx, deploymentID)
	return getErr == nil && current.Status == models.DeploymentStatusCanceled
}

func (s *Service) recordLog(ctx context.Context, deploymentID, level, source, message string) {
	if logErr := s.Log(ctx, deploymentID, level, source, message); logErr != nil && s.logger != nil {
		s.logger.Error("failed to record deployment log", "deployment_id", deploymentID, "error", logErr)
	}
}

func (s *Service) cloneForDeployment(ctx context.Context, d *models.Deployment, p *models.Project, env *models.Environment, accessToken string) (string, error) {
	if tErr := s.Transition(ctx, d.ID, models.DeploymentStatusCloning, nil); tErr != nil {
		return "", tErr
	}
	s.recordLog(ctx, d.ID, "info", "clone", "cloning repository")

	workDir := filepath.Join(s.buildDir, "goforge-builds", d.ID)
	if mErr := os.MkdirAll(workDir, 0o700); mErr != nil {
		return "", mErr
	}

	branch := p.DefaultBranch
	if env.Branch.Valid && env.Branch.String != "" {
		branch = env.Branch.String
	}

	cloneLog := newLogWriter(ctx, d.ID, "clone", s.Log)
	if cErr := s.gitService.Clone(ctx, git.CloneOptions{
		URL:         p.RepositoryURL,
		Branch:      branch,
		Path:        workDir,
		Depth:       1,
		AccessToken: accessToken,
		Progress:    cloneLog,
	}); cErr != nil {
		cloneLog.Flush()
		os.RemoveAll(workDir)
		return "", fmt.Errorf("git clone failed: %w", cErr)
	}
	cloneLog.Flush()
	return workDir, nil
}

func (s *Service) buildForDeployment(ctx context.Context, d *models.Deployment, p *models.Project, workDir string) (string, error) {
	if tErr := s.Transition(ctx, d.ID, models.DeploymentStatusBuilding, nil); tErr != nil {
		return "", tErr
	}
	s.recordLog(ctx, d.ID, "info", "build", "building image")

	buildArgs, bArgsErr := buildArgsFromProject(p)
	if bArgsErr != nil {
		return "", fmt.Errorf("invalid build args: %w", bArgsErr)
	}

	spec, specErr := ParseBuildSpec(p.DockerfilePath, p.BuildContext)
	if specErr != nil {
		return "", specErr
	}
	contextDir, dockerfile, locErr := spec.Locate(workDir)
	if locErr != nil {
		return "", locErr
	}

	imageName := fmt.Sprintf("goforge-%s:%s", p.ID, d.ID)
	buildLog := newLogWriter(ctx, d.ID, "build", s.Log)
	if bErr := s.dockerClient.BuildImage(ctx, docker.BuildOptions{
		ImageName:  imageName,
		Dockerfile: dockerfile,
		ContextDir: contextDir,
		BuildArgs:  buildArgs,
	}, buildLog); bErr != nil {
		buildLog.Flush()
		return "", fmt.Errorf("docker build failed: %w", bErr)
	}
	buildLog.Flush()
	return imageName, nil
}

func deploymentDomain(p *models.Project, env *models.Environment) string {
	if env != nil && env.Domain.Valid && env.Domain.String != "" {
		return env.Domain.String
	}
	if p != nil && p.CustomDomain.Valid {
		return p.CustomDomain.String
	}
	return ""
}

func buildArgsFromProject(p *models.Project) (map[string]*string, error) {
	if p == nil {
		return nil, nil
	}
	raw, err := p.GetBuildArgs()
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	args := make(map[string]*string, len(raw))
	for key, value := range raw {
		copied := value
		args[key] = &copied
	}
	return args, nil
}

// buildContainerEnv collects environment variables from user-defined env vars
// and linked service connection strings for injection into the deployment container.
func (s *Service) buildContainerEnv(ctx context.Context, environmentID, projectID string) []string {
	var env []string

	if s.envVariableRepo != nil {
		vars, err := s.envVariableRepo.ListByEnvironmentID(ctx, environmentID)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("failed to fetch env variables for deployment", "environment_id", environmentID, "error", err)
			}
		} else {
			for _, v := range vars {
				env = append(env, v.Key+"="+v.Value)
			}
		}
	}

	if s.projectServiceInstanceRepo != nil {
		instances, err := s.projectServiceInstanceRepo.ListByProjectID(ctx, projectID)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("failed to fetch linked services for deployment", "project_id", projectID, "error", err)
			}
		} else {
			for _, inst := range instances {
				connInfo, ciErr := inst.GetConnectionInfo()
				if ciErr != nil {
					if s.logger != nil {
						s.logger.Warn("failed to parse connection info", "instance_id", inst.ID, "error", ciErr)
					}
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
		return fmt.Errorf("health check failed: could not inspect container: %w", inspectErr)
	}

	ipAddress := ""
	if network, ok := containerInfo.NetworkSettings.Networks[s.traefikNetwork]; ok && network.IPAddress.IsValid() {
		ipAddress = network.IPAddress.String()
	}
	if ipAddress == "" {
		return fmt.Errorf("health check failed: container has no IP on network %s", s.traefikNetwork)
	}
	if !env.Port.Valid {
		return fmt.Errorf("health check failed: container port is required")
	}

	healthCheckURL := fmt.Sprintf("http://%s:%d", ipAddress, env.Port.Int32)
	s.recordLog(ctx, deploymentID, "info", "health", "checking "+healthCheckURL+p.HealthCheckPath)

	req := BlueGreenRequest{
		DeploymentID:    deploymentID,
		NewContainerID:  containerID,
		OldContainerID:  "",
		HealthCheckURL:  healthCheckURL,
		HealthCheckPath: p.HealthCheckPath,
	}

	_, bgErr := s.blueGreenDeployer.Deploy(ctx, req)
	if bgErr != nil {
		if s.logger != nil {
			s.logger.Error("health check failed, rolling back", "deployment_id", deploymentID, "error", bgErr)
		}
		if stopErr := s.dockerClient.StopContainer(ctx, containerID); stopErr != nil && s.logger != nil {
			s.logger.Error("failed to stop container during rollback", "container_id", containerID, "error", stopErr)
		}
		if s.containerRepo != nil {
			containers, listErr := s.containerRepo.ListByDeploymentID(ctx, deploymentID)
			if listErr != nil && s.logger != nil {
				s.logger.Warn("failed to list containers during rollback", "deployment_id", deploymentID, "error", listErr)
			}
			for _, c := range containers {
				if updateErr := s.containerRepo.UpdateStatus(ctx, c.ID, models.ContainerStatusExited); updateErr != nil && s.logger != nil {
					s.logger.Warn("failed to update container status during rollback", "container_id", c.ID, "error", updateErr)
				}
			}
		}
		return fmt.Errorf("health check failed: %w", bgErr)
	}

	return nil
}
