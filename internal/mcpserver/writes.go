package mcpserver

import (
	"context"
	"net/http"
)

// writeToolNames is every tool registerWrites adds. TestWriteToolNames keeps
// it in sync.
var writeToolNames = []string{
	"trigger_deployment", "cancel_deployment", "rollback_deployment", "set_env_var",
	"delete_env_var", "sync_env_vars", "link_service", "unlink_service", "deploy_service",
	"restart_service", "update_service_image", "update_service_config", "restart_container",
	"stop_container",
}

// Gate is part of every mutation input.
type Gate struct {
	DryRun      *bool  `json:"dry_run,omitempty" jsonschema:"default true: only return the plan. Set false to run the change."`
	ApprovalRef string `json:"approval_ref,omitempty" jsonschema:"required when dry_run is false: the id of the human approval (for example a Tater message id). Written to the audit log. A retry with the same approval_ref on the same target does not run the change again; it returns the first result and audit_id."`
}

func (g Gate) body(extra map[string]any) map[string]any {
	b := map[string]any{}
	for k, v := range extra {
		b[k] = v
	}
	if g.DryRun != nil {
		b["dry_run"] = *g.DryRun
	}
	if g.ApprovalRef != "" {
		b["approval_ref"] = g.ApprovalRef
	}
	return b
}

type triggerInput struct {
	Gate
	ProjectID     string `json:"project_id" jsonschema:"project UUID"`
	EnvironmentID string `json:"environment_id,omitempty" jsonschema:"environment UUID (default: the project's first environment)"`
}

type deploymentGateInput struct {
	Gate
	ProjectID    string `json:"project_id" jsonschema:"project UUID"`
	DeploymentID string `json:"deployment_id" jsonschema:"deployment UUID"`
}

type rollbackInput struct {
	Gate
	ProjectID    string `json:"project_id" jsonschema:"project UUID"`
	DeploymentID string `json:"deployment_id" jsonschema:"a deployment of the environment to roll back"`
	Reason       string `json:"reason,omitempty" jsonschema:"why, recorded on the rollback"`
}

type serviceGateInput struct {
	Gate
	ServiceID string `json:"service_id" jsonschema:"service instance UUID"`
}

type containerGateInput struct {
	Gate
	ContainerID string `json:"container_id" jsonschema:"Docker container ID"`
}

type setEnvVarInput struct {
	Gate
	ProjectID     string `json:"project_id" jsonschema:"project UUID"`
	EnvironmentID string `json:"environment_id" jsonschema:"environment UUID"`
	Key           string `json:"key" jsonschema:"variable name, [A-Za-z_][A-Za-z0-9_]*"`
	Value         string `json:"value" jsonschema:"variable value; write-only, never echoed or audited"`
	IsSecret      bool   `json:"is_secret,omitempty" jsonschema:"mask the value in every read"`
}

type envKeyInput struct {
	Gate
	ProjectID     string `json:"project_id" jsonschema:"project UUID"`
	EnvironmentID string `json:"environment_id" jsonschema:"environment UUID"`
	Key           string `json:"key" jsonschema:"variable name"`
}

type envGateInput struct {
	Gate
	ProjectID     string `json:"project_id" jsonschema:"project UUID"`
	EnvironmentID string `json:"environment_id" jsonschema:"environment UUID"`
}

type linkInput struct {
	Gate
	ProjectID string `json:"project_id" jsonschema:"project UUID"`
	ServiceID string `json:"service_id" jsonschema:"service instance UUID"`
}

type deployServiceInput struct {
	Gate
	Template string         `json:"template" jsonschema:"service template name"`
	Name     string         `json:"name" jsonschema:"instance name"`
	Domain   string         `json:"domain,omitempty" jsonschema:"public domain for web services"`
	Config   map[string]any `json:"config,omitempty" jsonschema:"template config fields; omitted fields use defaults or are generated"`
}

type updateConfigInput struct {
	Gate
	ServiceID string         `json:"service_id" jsonschema:"service instance UUID"`
	Config    map[string]any `json:"config" jsonschema:"fields to change; immutable fields are refused; a blank password keeps the stored one"`
}

func post(path string, body any) *call { return &call{method: http.MethodPost, path: path, body: body} }

func projectEnvPath(projectID, envID string) (string, error) {
	if err := required("project_id", projectID); err != nil {
		return "", err
	}
	if err := required("environment_id", envID); err != nil {
		return "", err
	}
	return "/projects/" + seg(projectID) + "/environments/" + seg(envID), nil
}

func deploymentPath(projectID, deploymentID string) (string, error) {
	if err := required("project_id", projectID); err != nil {
		return "", err
	}
	if err := required("deployment_id", deploymentID); err != nil {
		return "", err
	}
	return "/projects/" + seg(projectID) + "/deployments/" + seg(deploymentID), nil
}

const gateNote = " Defaults to a dry run that returns the plan; run it with dry_run=false and approval_ref."

func (s *Server) registerWrites() {
	addTool(s, "trigger_deployment", "Queue a manual deployment of a project environment."+gateNote, nonDestruct,
		func(_ context.Context, in triggerInput) (*call, error) {
			body := in.body(nil)
			if in.EnvironmentID != "" {
				body["env_id"] = in.EnvironmentID
			}
			return post("/projects/"+seg(in.ProjectID)+"/deployments", body), required("project_id", in.ProjectID)
		})
	addTool(s, "cancel_deployment", "Cancel a pending or running deployment."+gateNote, destructive,
		func(_ context.Context, in deploymentGateInput) (*call, error) {
			p, err := deploymentPath(in.ProjectID, in.DeploymentID)
			return post(p+"/cancel", in.body(nil)), err
		})
	addTool(s, "rollback_deployment", "Roll the deployment's environment back to its previous successful deployment."+gateNote, destructive,
		func(_ context.Context, in rollbackInput) (*call, error) {
			p, err := deploymentPath(in.ProjectID, in.DeploymentID)
			body := in.body(nil)
			if in.Reason != "" {
				body["reason"] = in.Reason
			}
			return post(p+"/rollback", body), err
		})

	addTool(s, "set_env_var", "Create or update an environment variable. Takes effect on the next deployment."+gateNote, nonDestruct,
		func(_ context.Context, in setEnvVarInput) (*call, error) {
			p, err := projectEnvPath(in.ProjectID, in.EnvironmentID)
			if err == nil {
				err = required("key", in.Key)
			}
			return post(p+"/variables", in.body(map[string]any{"key": in.Key, "value": in.Value, "is_secret": in.IsSecret})), err
		})
	addTool(s, "delete_env_var", "Delete an environment variable by key."+gateNote, destructive,
		func(_ context.Context, in envKeyInput) (*call, error) {
			p, err := projectEnvPath(in.ProjectID, in.EnvironmentID)
			if err == nil {
				err = required("key", in.Key)
			}
			return &call{method: http.MethodDelete, path: p + "/variables/" + seg(in.Key), body: in.body(nil)}, err
		})
	addTool(s, "sync_env_vars", "Add variables declared in the repo's .env.example that the environment lacks."+gateNote, nonDestruct,
		func(_ context.Context, in envGateInput) (*call, error) {
			p, err := projectEnvPath(in.ProjectID, in.EnvironmentID)
			return post(p+"/variables/sync", in.body(nil)), err
		})

	addTool(s, "link_service", "Link a service instance to a project and add its connection variables."+gateNote, nonDestruct,
		func(_ context.Context, in linkInput) (*call, error) {
			err := required("project_id", in.ProjectID)
			if err == nil {
				err = required("service_id", in.ServiceID)
			}
			return post("/projects/"+seg(in.ProjectID)+"/services", in.body(map[string]any{"service_instance_id": in.ServiceID})), err
		})
	addTool(s, "unlink_service", "Unlink a service instance from a project. The service keeps running."+gateNote, destructive,
		func(_ context.Context, in linkInput) (*call, error) {
			err := required("project_id", in.ProjectID)
			if err == nil {
				err = required("service_id", in.ServiceID)
			}
			return &call{method: http.MethodDelete, path: "/projects/" + seg(in.ProjectID) + "/services/" + seg(in.ServiceID), body: in.body(nil)}, err
		})

	addTool(s, "deploy_service", "Deploy a new one-click service instance from a template."+gateNote, nonDestruct,
		func(_ context.Context, in deployServiceInput) (*call, error) {
			extra := map[string]any{"template": in.Template, "name": in.Name}
			if in.Domain != "" {
				extra["domain"] = in.Domain
			}
			if len(in.Config) > 0 {
				extra["config"] = in.Config
			}
			return post("/services", in.body(extra)), required("template", in.Template, "name", in.Name)
		})
	addTool(s, "restart_service", "Restart a service's container in place. Allowed on protected services."+gateNote, nonDestruct,
		func(_ context.Context, in serviceGateInput) (*call, error) {
			return post("/services/"+seg(in.ServiceID)+"/restart", in.body(nil)), required("service_id", in.ServiceID)
		})
	addTool(s, "update_service_image", "Pull the service image again and recreate its container; volumes are kept. Protected services need the protected scope."+gateNote, destructive,
		func(ctx context.Context, in serviceGateInput) (*call, error) {
			if err := required("service_id", in.ServiceID); err != nil {
				return nil, err
			}
			if err := s.refuseProtected(ctx, in.ServiceID, "update_service_image"); err != nil {
				return nil, err
			}
			return post("/services/"+seg(in.ServiceID)+"/image", in.body(nil)), nil
		})
	addTool(s, "update_service_config", "Change a service's config and recreate its container. Returns a masked diff. Immutable fields are refused. Protected services need the protected scope."+gateNote, destructive,
		func(ctx context.Context, in updateConfigInput) (*call, error) {
			if err := required("service_id", in.ServiceID); err != nil {
				return nil, err
			}
			if err := s.refuseProtected(ctx, in.ServiceID, "update_service_config"); err != nil {
				return nil, err
			}
			return &call{method: http.MethodPatch, path: "/services/" + seg(in.ServiceID) + "/config", body: in.body(map[string]any{"config": in.Config})}, nil
		})

	addTool(s, "restart_container", "Restart a container in place. Allowed on protected services."+gateNote, nonDestruct,
		func(_ context.Context, in containerGateInput) (*call, error) {
			return post("/containers/"+seg(in.ContainerID)+"/restart", in.body(nil)), required("container_id", in.ContainerID)
		})
	addTool(s, "stop_container", "Stop a container. Protected services need the protected scope."+gateNote, destructive,
		func(ctx context.Context, in containerGateInput) (*call, error) {
			if err := required("container_id", in.ContainerID); err != nil {
				return nil, err
			}
			serviceID, err := s.containerService(ctx, in.ContainerID)
			if err != nil {
				return nil, err
			}
			if err := s.refuseProtected(ctx, serviceID, "stop_container"); err != nil {
				return nil, err
			}
			return post("/containers/"+seg(in.ContainerID)+"/stop", in.body(nil)), nil
		})
}
