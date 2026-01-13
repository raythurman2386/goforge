// Package proxy provides proxy configuration and label generation.
package proxy

import (
	"fmt"

	"github.com/raythurman2386/gocool/internal/models"
)

// Labels represents Traefik Docker labels.
type Labels map[string]string

// GenerateLabels generates Traefik labels for an environment.
func GenerateLabels(p *models.Project, env *models.Environment, network string) Labels {
	name := fmt.Sprintf("gocool-%s-%s", p.ID, env.Name)
	labels := Labels{
		"traefik.enable":         "true",
		"traefik.docker.network": network,
		"gocool.managed":         "true",
		"gocool.project.id":      p.ID,
		"gocool.environment.id":  env.ID,
	}

	if env.Domain.Valid && env.Domain.String != "" {
		labels[fmt.Sprintf("traefik.http.routers.%s.rule", name)] = fmt.Sprintf("Host(`%s`)", env.Domain.String)
		labels[fmt.Sprintf("traefik.http.routers.%s.entrypoints", name)] = "websecure"
		labels[fmt.Sprintf("traefik.http.routers.%s.tls.certresolver", name)] = "letsencrypt"

		port := 80
		if env.Port.Valid {
			port = int(env.Port.Int32)
		}
		labels[fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", name)] = fmt.Sprintf("%d", port)
	}

	return labels
}
