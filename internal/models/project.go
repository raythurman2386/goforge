package models

import (
	"database/sql"
	"encoding/json"
	"time"
)

// Project represents a deployable project.
type Project struct {
	ID                  string
	UserID              string
	Name                string
	Description         sql.NullString
	GitSourceID         sql.NullString
	RepositoryURL       string
	RepositoryName      sql.NullString // owner/repo format
	DefaultBranch       string
	DockerfilePath      string
	DockerComposePath   sql.NullString
	BuildContext        string
	BuildArgs           json.RawMessage // JSONB
	HealthCheckPath     string
	HealthCheckInterval int // seconds
	AutoDeploy          bool
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// GetBuildArgs parses the build args JSON into a map.
func (p *Project) GetBuildArgs() (map[string]string, error) {
	if len(p.BuildArgs) == 0 {
		return make(map[string]string), nil
	}
	var args map[string]string
	if err := json.Unmarshal(p.BuildArgs, &args); err != nil {
		return nil, err
	}
	return args, nil
}

// SetBuildArgs sets the build args from a map.
func (p *Project) SetBuildArgs(args map[string]string) error {
	data, err := json.Marshal(args)
	if err != nil {
		return err
	}
	p.BuildArgs = data
	return nil
}
