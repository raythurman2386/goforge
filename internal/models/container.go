package models

import (
	"database/sql"
	"encoding/json"
	"time"
)

// ContainerStatus represents the status of a container.
type ContainerStatus string

const (
	ContainerStatusCreated    ContainerStatus = "created"
	ContainerStatusRunning    ContainerStatus = "running"
	ContainerStatusPaused     ContainerStatus = "paused"
	ContainerStatusRestarting ContainerStatus = "restarting"
	ContainerStatusRemoving   ContainerStatus = "removing"
	ContainerStatusExited     ContainerStatus = "exited"
	ContainerStatusDead       ContainerStatus = "dead"
)

// PortMapping represents a port mapping between host and container.
type PortMapping struct {
	Host      int    `json:"host"`
	Container int    `json:"container"`
	Protocol  string `json:"protocol,omitempty"`
}

// Container represents a Docker container.
type Container struct {
	ID                string
	DeploymentID      sql.NullString
	ServiceInstanceID sql.NullString
	DockerID          string
	Name              string
	Image             string
	Status            ContainerStatus
	Ports             json.RawMessage // []PortMapping
	Labels            json.RawMessage // map[string]string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// GetPorts parses the ports JSON into a slice.
func (c *Container) GetPorts() ([]PortMapping, error) {
	if len(c.Ports) == 0 {
		return nil, nil
	}
	var ports []PortMapping
	if err := json.Unmarshal(c.Ports, &ports); err != nil {
		return nil, err
	}
	return ports, nil
}

// GetLabels parses the labels JSON into a map.
func (c *Container) GetLabels() (map[string]string, error) {
	if len(c.Labels) == 0 {
		return make(map[string]string), nil
	}
	var labels map[string]string
	if err := json.Unmarshal(c.Labels, &labels); err != nil {
		return nil, err
	}
	return labels, nil
}

// ContainerMetrics represents resource usage metrics for a container.
type ContainerMetrics struct {
	ID            string
	ContainerID   string
	Timestamp     time.Time
	CPUPercent    float64
	MemoryUsage   int64
	MemoryLimit   int64
	MemoryPercent float64
	NetworkRx     int64
	NetworkTx     int64
	BlockRead     int64
	BlockWrite    int64
}
