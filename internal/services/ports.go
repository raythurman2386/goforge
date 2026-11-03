package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/proxy"
)

// protoTCP is the protocol a port gets when the template leaves it empty.
const protoTCP = "tcp"

// DefaultPublishHostIP is the host address a published template port binds
// when the template does not name one. Loopback keeps the port off the LAN
// and the internet; reach it through an SSH jump to the host.
const DefaultPublishHostIP = "127.0.0.1"

// validatePorts checks the template's docker.ports. A published port needs a
// host_port that parses as a template, and its host_ip must be a loopback
// address: 0.0.0.0, ::, and any other address are refused, because Docker
// publishes bypass the host firewall.
func (t *ServiceTemplate) validatePorts() error {
	var errs []error
	for i, p := range t.Docker.Ports {
		where := fmt.Sprintf("docker.ports[%d] (container %d)", i, p.Container)
		if p.Container < 1 || p.Container > 65535 {
			errs = append(errs, fmt.Errorf("%s: container port must be 1-65535", where))
		}
		switch strings.ToLower(p.Protocol) {
		case "", protoTCP, "udp":
		default:
			errs = append(errs, fmt.Errorf("%s: protocol %q must be tcp or udp", where, p.Protocol))
		}
		if p.Domain != "" {
			if i == 0 {
				errs = append(errs, fmt.Errorf("%s: domain is for extra ports; the instance Domain routes the first port", where))
			} else if _, err := template.New("domain").Funcs(templateFuncs).Parse(p.Domain); err != nil {
				errs = append(errs, fmt.Errorf("%s: domain: %w", where, err))
			}
		}
		if !p.Publish {
			if p.HostPort != "" || p.HostIP != "" {
				errs = append(errs, fmt.Errorf("%s: host_port and host_ip need publish: true", where))
			}
			continue
		}
		if strings.TrimSpace(p.HostPort) == "" {
			errs = append(errs, fmt.Errorf("%s: publish needs host_port", where))
		} else if _, err := template.New("host_port").Funcs(templateFuncs).Parse(p.HostPort); err != nil {
			errs = append(errs, fmt.Errorf("%s: host_port: %w", where, err))
		}
		if err := checkPublishHostIP(p.HostIP); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", where, err))
		}
	}
	return errors.Join(errs...)
}

// checkPublishHostIP accepts an empty host IP (it defaults to
// DefaultPublishHostIP) or a loopback address. It refuses 0.0.0.0 and ::
// outright.
func checkPublishHostIP(hostIP string) error {
	if hostIP == "" {
		return nil
	}
	ip := net.ParseIP(hostIP)
	switch {
	case ip == nil:
		return fmt.Errorf("host_ip %q is not an IP address", hostIP)
	case ip.IsUnspecified():
		return fmt.Errorf("host_ip %q publishes on every interface and is refused; use 127.0.0.1", hostIP)
	case !ip.IsLoopback():
		return fmt.Errorf("host_ip %q is refused; published ports bind loopback only (127.0.0.1)", hostIP)
	}
	return nil
}

// applyPortDefaults fills host_ip on published ports so it is never empty.
func (t *ServiceTemplate) applyPortDefaults() {
	for i := range t.Docker.Ports {
		p := &t.Docker.Ports[i]
		if p.Publish && p.HostIP == "" {
			p.HostIP = DefaultPublishHostIP
		}
		if p.Publish && p.Protocol == "" {
			p.Protocol = protoTCP
		}
	}
}

// renderPortBindings renders the host_port of every published port with data
// and returns the bindings. A host port that does not render to 1-65535 is a
// config error (ErrInvalidConfig). Templates with no published ports return nil.
func (s *InstanceService) renderPortBindings(tmpl *ServiceTemplate, data map[string]interface{}) ([]docker.PortBinding, error) {
	var out []docker.PortBinding //nolint:prealloc // nil when nothing is published
	seen := make(map[string]int)
	for _, p := range tmpl.Docker.Ports {
		if !p.Publish {
			continue
		}
		raw, err := s.interpolate(p.HostPort, data)
		if err != nil {
			return nil, fmt.Errorf("failed to interpolate host_port for container port %d: %w", p.Container, err)
		}
		hostPort := strings.TrimSpace(raw)
		n, convErr := strconv.Atoi(hostPort)
		if convErr != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("%w: host port %q for container port %d must be a number from 1 to 65535", ErrInvalidConfig, hostPort, p.Container)
		}
		hostIP := p.HostIP
		if hostIP == "" {
			hostIP = DefaultPublishHostIP
		}
		if ipErr := checkPublishHostIP(hostIP); ipErr != nil {
			return nil, fmt.Errorf("container port %d: %w", p.Container, ipErr)
		}
		proto := strings.ToLower(p.Protocol)
		if proto == "" {
			proto = protoTCP
		}
		key := hostIP + ":" + strconv.Itoa(n) + "/" + proto
		if prev, dup := seen[key]; dup {
			return nil, fmt.Errorf("%w: container ports %d and %d both publish on %s", ErrInvalidConfig, prev, p.Container, key)
		}
		seen[key] = p.Container
		out = append(out, docker.PortBinding{
			ContainerPort: strconv.Itoa(p.Container),
			Protocol:      proto,
			HostIP:        hostIP,
			HostPort:      strconv.Itoa(n),
		})
	}
	return out, nil
}

// hostnamePattern matches an RFC 1123 hostname, so a rendered port domain can
// go into a Traefik Host() rule unquoted.
var hostnamePattern = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)*[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

// renderPortRoutes renders the domain of every port after the first and
// returns one Traefik route per port whose domain is not empty. It returns
// nil when the instance has no Domain, since Traefik is then off for the
// container. A domain that is not a hostname, or that repeats the instance
// Domain or another port's domain, is a config error (ErrInvalidConfig).
func (s *InstanceService) renderPortRoutes(tmpl *ServiceTemplate, data map[string]interface{}) ([]proxy.ExtraRoute, error) {
	primary := dataString(data, "Domain")
	if primary == "" {
		return nil, nil
	}
	var out []proxy.ExtraRoute //nolint:prealloc // nil when no port has a domain
	seen := map[string]bool{strings.ToLower(primary): true}
	for i, p := range tmpl.Docker.Ports {
		if i == 0 || p.Domain == "" {
			continue
		}
		raw, err := s.interpolate(p.Domain, data)
		if err != nil {
			return nil, fmt.Errorf("failed to interpolate domain for container port %d: %w", p.Container, err)
		}
		host := strings.TrimSpace(raw)
		if host == "" {
			continue
		}
		if len(host) > 253 || !hostnamePattern.MatchString(host) {
			return nil, fmt.Errorf("%w: domain %q for container port %d is not a valid hostname", ErrInvalidConfig, host, p.Container)
		}
		if seen[strings.ToLower(host)] {
			return nil, fmt.Errorf("%w: domain %q for container port %d is already used by this service", ErrInvalidConfig, host, p.Container)
		}
		seen[strings.ToLower(host)] = true
		out = append(out, proxy.ExtraRoute{Domain: host, Port: p.Container})
	}
	return out, nil
}

// portMappingsJSON is the containers.ports value for bindings: a JSON array
// of models.PortMapping, "[]" when nothing is published.
func portMappingsJSON(bindings []docker.PortBinding) json.RawMessage {
	mappings := make([]models.PortMapping, 0, len(bindings))
	for _, b := range bindings {
		host, hostErr := strconv.Atoi(b.HostPort)
		ctr, ctrErr := strconv.Atoi(b.ContainerPort)
		if hostErr != nil || ctrErr != nil {
			continue // renderPortBindings only produces numeric ports
		}
		mappings = append(mappings, models.PortMapping{Host: host, Container: ctr, Protocol: b.Protocol, HostIP: b.HostIP})
	}
	data, err := json.Marshal(mappings)
	if err != nil {
		return json.RawMessage("[]")
	}
	return data
}

// PublishedPortRows lists a service container's published ports for the
// service page, one row per binding, such as "22/tcp" -> "127.0.0.1:2223".
func PublishedPortRows(c *models.Container) []DetailRow {
	if c == nil {
		return nil
	}
	ports, err := c.GetPorts()
	if err != nil {
		return nil
	}
	rows := make([]DetailRow, 0, len(ports))
	for _, p := range ports {
		proto := p.Protocol
		if proto == "" {
			proto = protoTCP
		}
		hostIP := p.HostIP
		if hostIP == "" {
			hostIP = "0.0.0.0"
		}
		rows = append(rows, DetailRow{
			Key:     fmt.Sprintf("published %d/%s", p.Container, proto),
			Display: net.JoinHostPort(hostIP, strconv.Itoa(p.Host)),
		})
	}
	return rows
}

// dataString returns data[key] when it is a string, else "".
func dataString(data map[string]interface{}, key string) string {
	if v, ok := data[key].(string); ok {
		return v
	}
	return ""
}
