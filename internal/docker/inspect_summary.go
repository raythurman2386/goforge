package docker

import (
	"sort"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
)

// InspectSummary is what Docker inspect says about a container, minus
// anything that can hold a secret: the environment, command, and args are
// never copied, and only traefik.enable and router rules are read from labels.
// The JSON tags are the /api/v1 container state shape; the web UI renders the
// same struct.
type InspectSummary struct {
	DockerID      string            `json:"docker_id"`
	Name          string            `json:"name"`
	Image         string            `json:"image"`
	State         string            `json:"state"`
	Running       bool              `json:"running"`
	Health        string            `json:"health,omitempty"`
	RestartCount  int               `json:"restart_count"`
	ExitCode      int               `json:"exit_code"`
	OOMKilled     bool              `json:"oom_killed"`
	StartedAt     string            `json:"started_at,omitempty"`
	FinishedAt    string            `json:"finished_at,omitempty"`
	Ports         []InspectPort     `json:"ports"`
	TraefikEnable bool              `json:"traefik_enable"`
	TraefikRules  map[string]string `json:"traefik_rules,omitempty"`
	Volumes       []InspectMount    `json:"volumes"`
	Limits        InspectLimits     `json:"limits"`
	Networks      []string          `json:"networks"`
}

// InspectPort is one container port and, when published, its host binding.
type InspectPort struct {
	Container string `json:"container"`
	HostIP    string `json:"host_ip,omitempty"`
	HostPort  string `json:"host_port,omitempty"`
}

// InspectMount is one mount on the container.
type InspectMount struct {
	Type        string `json:"type"`
	Name        string `json:"name,omitempty"`
	Destination string `json:"destination"`
	ReadWrite   bool   `json:"read_write"`
}

// InspectLimits are the memory and CPU limits set on the container. Zero
// means unlimited.
type InspectLimits struct {
	MemoryBytes int64   `json:"memory_bytes"`
	CPUs        float64 `json:"cpus"`
}

// appendInspectPorts flattens a port map into rows; an exposed port with
// no host binding gets one row with only Container set.
func appendInspectPorts(out []InspectPort, ports network.PortMap) []InspectPort {
	for port, bindings := range ports {
		if len(bindings) == 0 {
			out = append(out, InspectPort{Container: port.String()})
		}
		for _, b := range bindings {
			hostIP := ""
			if b.HostIP.IsValid() {
				hostIP = b.HostIP.String()
			}
			out = append(out, InspectPort{Container: port.String(), HostIP: hostIP, HostPort: b.HostPort})
		}
	}
	return out
}

// SummarizeInspect maps an inspect response to its secret-free summary.
// An inspect response without an ID carries no container and returns nil.
func SummarizeInspect(info *container.InspectResponse) *InspectSummary {
	if info == nil || info.ID == "" {
		return nil
	}
	out := &InspectSummary{
		DockerID:     info.ID,
		Name:         strings.TrimPrefix(info.Name, "/"),
		RestartCount: info.RestartCount,
		Ports:        []InspectPort{},
		Volumes:      []InspectMount{},
		Networks:     []string{},
	}
	if info.Config != nil {
		out.Image = info.Config.Image
		out.TraefikEnable = info.Config.Labels["traefik.enable"] == "true"
		for k, v := range info.Config.Labels {
			if strings.HasPrefix(k, "traefik.http.routers.") && strings.HasSuffix(k, ".rule") {
				if out.TraefikRules == nil {
					out.TraefikRules = map[string]string{}
				}
				out.TraefikRules[k] = v
			}
		}
	}
	if s := info.State; s != nil {
		out.State = string(s.Status)
		out.Running = s.Running
		out.ExitCode = s.ExitCode
		out.OOMKilled = s.OOMKilled
		out.StartedAt = s.StartedAt
		out.FinishedAt = s.FinishedAt
		if s.Health != nil {
			out.Health = string(s.Health.Status)
		}
	}
	if info.HostConfig != nil {
		out.Limits = InspectLimits{MemoryBytes: info.HostConfig.Memory, CPUs: float64(info.HostConfig.NanoCPUs) / 1e9}
	}
	if info.NetworkSettings != nil {
		out.Ports = appendInspectPorts(out.Ports, info.NetworkSettings.Ports)
		for name := range info.NetworkSettings.Networks {
			out.Networks = append(out.Networks, name)
		}
	}
	sort.Slice(out.Ports, func(i, j int) bool {
		return out.Ports[i].Container+out.Ports[i].HostPort < out.Ports[j].Container+out.Ports[j].HostPort
	})
	sort.Strings(out.Networks)
	for _, m := range info.Mounts {
		out.Volumes = append(out.Volumes, InspectMount{Type: string(m.Type), Name: m.Name, Destination: m.Destination, ReadWrite: m.RW})
	}
	return out
}

// Started parses StartedAt. ok is false when Docker reported no start time.
func (s *InspectSummary) Started() (t time.Time, ok bool) {
	if s == nil || s.StartedAt == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s.StartedAt)
	if err != nil || t.IsZero() || t.Year() < 2000 {
		return time.Time{}, false
	}
	return t, true
}

// HostRules lists the Traefik router rules in a stable order.
func (s *InspectSummary) HostRules() []string {
	if s == nil || len(s.TraefikRules) == 0 {
		return nil
	}
	keys := make([]string, 0, len(s.TraefikRules))
	for k := range s.TraefikRules {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, s.TraefikRules[k])
	}
	return out
}
