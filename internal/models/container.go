package models

import (
	"database/sql"
	"encoding/json"
	"time"
)

// ContainerStatus represents the status of a container.
type ContainerStatus string

const (
	// ContainerStatusCreated indicates the container has been created but not started.
	ContainerStatusCreated ContainerStatus = "created"
	// ContainerStatusRunning indicates the container is currently running.
	ContainerStatusRunning ContainerStatus = "running"
	// ContainerStatusPaused indicates the container is paused.
	ContainerStatusPaused ContainerStatus = "paused"
	// ContainerStatusRestarting indicates the container is in the process of restarting.
	ContainerStatusRestarting ContainerStatus = "restarting"
	// ContainerStatusRemoving indicates the container is being removed.
	ContainerStatusRemoving ContainerStatus = "removing"
	// ContainerStatusExited indicates the container has exited.
	ContainerStatusExited ContainerStatus = "exited"
	// ContainerStatusDead indicates the container is dead.
	ContainerStatusDead ContainerStatus = "dead"
)

// PortMapping represents a port mapping between host and container.
type PortMapping struct {
	Host      int    `json:"host"`
	Container int    `json:"container"`
	Protocol  string `json:"protocol,omitempty"`
	// HostIP is the host address the port is bound on, such as 127.0.0.1.
	HostIP string `json:"host_ip,omitempty"`
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
