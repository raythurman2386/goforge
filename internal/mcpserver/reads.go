package mcpserver

import (
	"context"
	"net/http"
	"strconv"
)

type noInput struct{}

type projectInput struct {
	ProjectID string `json:"project_id" jsonschema:"project UUID"`
}

type envInput struct {
	ProjectID     string `json:"project_id" jsonschema:"project UUID"`
	EnvironmentID string `json:"environment_id" jsonschema:"environment UUID"`
}

type listDeploymentsInput struct {
	ProjectID     string `json:"project_id" jsonschema:"project UUID"`
	EnvironmentID string `json:"environment_id,omitempty" jsonschema:"only this environment (optional)"`
	Limit         int    `json:"limit,omitempty" jsonschema:"max deployments to return (default 20, max 500)"`
}

type deploymentInput struct {
	ProjectID    string `json:"project_id" jsonschema:"project UUID"`
	DeploymentID string `json:"deployment_id" jsonschema:"deployment UUID"`
}

type deploymentLogsInput struct {
	ProjectID    string `json:"project_id" jsonschema:"project UUID"`
	DeploymentID string `json:"deployment_id" jsonschema:"deployment UUID"`
	Tail         int    `json:"tail,omitempty" jsonschema:"last N log lines (default 200, max 5000)"`
}

type templateInput struct {
	Name string `json:"name" jsonschema:"template name, for example postgres"`
}

type serviceInput struct {
	ServiceID string `json:"service_id" jsonschema:"service instance UUID"`
}

type containerInput struct {
	ContainerID string `json:"container_id" jsonschema:"Docker container ID"`
}

type containerLogsInput struct {
	ContainerID string `json:"container_id" jsonschema:"Docker container ID"`
	Tail        int    `json:"tail,omitempty" jsonschema:"last N log lines (default 200, max 5000)"`
	Since       string `json:"since,omitempty" jsonschema:"only lines since this time (RFC 3339 or a Go duration like 10m)"`
}

type auditInput struct {
	Limit        int    `json:"limit,omitempty" jsonschema:"max rows (default 50, max 500)"`
	Offset       int    `json:"offset,omitempty" jsonschema:"rows to skip"`
	ResourceType string `json:"resource_type,omitempty" jsonschema:"filter by resource type, for example service_instance"`
	ResourceID   string `json:"resource_id,omitempty" jsonschema:"filter by resource UUID"`
	Action       string `json:"action,omitempty" jsonschema:"filter by action, for example api_v1.restart_service"`
	AllUsers     bool   `json:"all_users,omitempty" jsonschema:"admins only: include every user's rows"`
}

type gitSourceInput struct {
	GitSourceID string `json:"git_source_id" jsonschema:"git source UUID"`
}

type branchesInput struct {
	GitSourceID string `json:"git_source_id" jsonschema:"git source UUID"`
	Repo        string `json:"repo" jsonschema:"repository full name, for example owner/repo"`
}

func intParam(n int) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func get(path string) *call { return &call{method: http.MethodGet, path: path} }

func (s *Server) registerReads() {
	addTool(s, "health", "GoForge server, database, and Docker health.", readOnly,
		func(context.Context, noInput) (*call, error) { return get("/health"), nil })
	addTool(s, "whoami", "The token's user, scopes, project and service restriction, grants, and the protected service list.", readOnly,
		func(context.Context, noInput) (*call, error) { return get("/whoami"), nil })
	addTool(s, "dashboard_stats", "Counts and resource totals for your projects, services, and containers.", readOnly,
		func(context.Context, noInput) (*call, error) { return get("/dashboard/stats"), nil })

	addTool(s, "list_projects", "List your projects.", readOnly,
		func(context.Context, noInput) (*call, error) { return get("/projects"), nil })
	addTool(s, "get_project", "One project with its environments and linked services.", readOnly,
		func(_ context.Context, in projectInput) (*call, error) {
			return get("/projects/" + seg(in.ProjectID)), required("project_id", in.ProjectID)
		})
	addTool(s, "list_environments", "List a project's environments.", readOnly,
		func(_ context.Context, in projectInput) (*call, error) {
			return get("/projects/" + seg(in.ProjectID) + "/environments"), required("project_id", in.ProjectID)
		})
	addTool(s, "list_env_vars", "List an environment's variables. Secret values are masked.", readOnly,
		func(_ context.Context, in envInput) (*call, error) {
			if err := required("project_id", in.ProjectID); err != nil {
				return nil, err
			}
			return get("/projects/" + seg(in.ProjectID) + "/environments/" + seg(in.EnvironmentID) + "/variables"),
				required("environment_id", in.EnvironmentID)
		})
	addTool(s, "list_deployments", "List a project's deployments, newest first.", readOnly,
		func(_ context.Context, in listDeploymentsInput) (*call, error) {
			c := get("/projects/" + seg(in.ProjectID) + "/deployments")
			c.query = map[string]string{"env_id": in.EnvironmentID, "limit": intParam(in.Limit)}
			return c, required("project_id", in.ProjectID)
		})
	addTool(s, "get_deployment", "One deployment's status, commit, and timing.", readOnly,
		func(_ context.Context, in deploymentInput) (*call, error) {
			if err := required("project_id", in.ProjectID); err != nil {
				return nil, err
			}
			return get("/projects/" + seg(in.ProjectID) + "/deployments/" + seg(in.DeploymentID)),
				required("deployment_id", in.DeploymentID)
		})
	addTool(s, "get_deployment_logs", "The tail of a deployment's build and deploy log.", readOnly,
		func(_ context.Context, in deploymentLogsInput) (*call, error) {
			if err := required("project_id", in.ProjectID); err != nil {
				return nil, err
			}
			c := get("/projects/" + seg(in.ProjectID) + "/deployments/" + seg(in.DeploymentID) + "/logs")
			c.query = map[string]string{"tail": intParam(in.Tail)}
			return c, required("deployment_id", in.DeploymentID)
		})

	addTool(s, "list_service_templates", "List the one-click service templates.", readOnly,
		func(context.Context, noInput) (*call, error) { return get("/service-templates"), nil })
	addTool(s, "get_service_template", "One service template's config fields, versions, and resources.", readOnly,
		func(_ context.Context, in templateInput) (*call, error) {
			return get("/service-templates/" + seg(in.Name)), required("name", in.Name)
		})
	addTool(s, "list_services", "List your service instances, with a protected flag on each.", readOnly,
		func(context.Context, noInput) (*call, error) { return get("/services"), nil })
	addTool(s, "get_service", "One service instance: masked config, immutable fields, volumes, and its container.", readOnly,
		func(_ context.Context, in serviceInput) (*call, error) {
			return get("/services/" + seg(in.ServiceID)), required("service_id", in.ServiceID)
		})

	addTool(s, "list_containers", "List your tracked containers.", readOnly,
		func(context.Context, noInput) (*call, error) { return get("/containers"), nil })
	addTool(s, "get_container", "One container's state, ports, mounts, limits, and networks (no env).", readOnly,
		func(_ context.Context, in containerInput) (*call, error) {
			return get("/containers/" + seg(in.ContainerID)), required("container_id", in.ContainerID)
		})
	addTool(s, "get_container_logs", "The tail of a container's logs. Known service secrets are redacted.", readOnly,
		func(_ context.Context, in containerLogsInput) (*call, error) {
			c := get("/containers/" + seg(in.ContainerID) + "/logs")
			c.query = map[string]string{"tail": intParam(in.Tail), "since": in.Since}
			return c, required("container_id", in.ContainerID)
		})
	addTool(s, "get_container_stats", "One sample of a container's CPU, memory, and network use.", readOnly,
		func(_ context.Context, in containerInput) (*call, error) {
			return get("/containers/" + seg(in.ContainerID) + "/stats"), required("container_id", in.ContainerID)
		})

	addTool(s, "get_backup_status", "Nightly S3 backup status (admin only): enabled, schedule, next run, last run, last success, recent runs. Read-only; there is no run-now tool.", readOnly,
		func(context.Context, noInput) (*call, error) { return get("/backups"), nil })

	s.registerWait()

	addTool(s, "list_audit_log", "Your audit log rows, newest first. Admins can pass all_users.", readOnly,
		func(_ context.Context, in auditInput) (*call, error) {
			c := get("/audit-log")
			c.query = map[string]string{
				"limit": intParam(in.Limit), "offset": intParam(in.Offset),
				"resource_type": in.ResourceType, "resource_id": in.ResourceID, "action": in.Action,
			}
			if in.AllUsers {
				c.query["all_users"] = "true"
			}
			return c, nil
		})

	addTool(s, "list_git_sources", "List your git sources. Credentials are never returned.", readOnly,
		func(context.Context, noInput) (*call, error) { return get("/git-sources"), nil })
	addTool(s, "list_repos", "List the repositories a git source can see.", readOnly,
		func(_ context.Context, in gitSourceInput) (*call, error) {
			return get("/git-sources/" + seg(in.GitSourceID) + "/repos"), required("git_source_id", in.GitSourceID)
		})
	addTool(s, "list_branches", "List a repository's branches through a git source.", readOnly,
		func(_ context.Context, in branchesInput) (*call, error) {
			if err := required("git_source_id", in.GitSourceID); err != nil {
				return nil, err
			}
			c := get("/git-sources/" + seg(in.GitSourceID) + "/branches")
			c.query = map[string]string{"repo": in.Repo}
			return c, required("repo", in.Repo)
		})
}
