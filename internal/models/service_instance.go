package models

import (
	"database/sql"
	"encoding/json"
	"time"
)

// ServiceInstanceStatus represents the status of a service instance.
type ServiceInstanceStatus string

const (
	// ServiceStatusPending indicates the service is being provisioned.
	ServiceStatusPending ServiceInstanceStatus = "pending"
	// ServiceStatusStarting indicates the service is starting up.
	ServiceStatusStarting ServiceInstanceStatus = "starting"
	// ServiceStatusRunning indicates the service is running.
	ServiceStatusRunning ServiceInstanceStatus = "running"
	// ServiceStatusStopped indicates the service is stopped.
	ServiceStatusStopped ServiceInstanceStatus = "stopped"
	// ServiceStatusFailed indicates the service has failed.
	ServiceStatusFailed ServiceInstanceStatus = "failed"
)

// ServiceInstance represents a deployed one-click service.
type ServiceInstance struct {
	ID             string
	UserID         string
	TemplateName   string
	Name           string
	Version        sql.NullString
	Config         json.RawMessage // template-specific configuration
	Domain         sql.NullString
	Status         ServiceInstanceStatus
	ConnectionInfo json.RawMessage // encrypted connection strings
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// GetConfig parses the config JSON into a map.
func (s *ServiceInstance) GetConfig() (map[string]interface{}, error) {
	if len(s.Config) == 0 {
		return make(map[string]interface{}), nil
	}
	var config map[string]interface{}
	if err := json.Unmarshal(s.Config, &config); err != nil {
		return nil, err
	}
	return config, nil
}

// GetConnectionInfo parses the connection info JSON into a map.
func (s *ServiceInstance) GetConnectionInfo() (map[string]string, error) {
	if len(s.ConnectionInfo) == 0 {
		return make(map[string]string), nil
	}
	var info map[string]string
	if err := json.Unmarshal(s.ConnectionInfo, &info); err != nil {
		return nil, err
	}
	return info, nil
}
