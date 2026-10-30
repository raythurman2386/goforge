// Package projects contains templates for project pages.
package projects

import (
	"fmt"

	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/web/templates/components/ui"
)

func getPort(env *models.Environment) string {
	if env.Port.Valid {
		return fmt.Sprintf("%d", env.Port.Int32)
	}
	return "80"
}

// serviceOptions lists service instances as "name (template)".
func serviceOptions(services []*models.ServiceInstance) []ui.SelectOption {
	opts := make([]ui.SelectOption, 0, len(services))
	for _, svc := range services {
		opts = append(opts, ui.SelectOption{Value: svc.ID, Label: svc.Name + " (" + svc.TemplateName + ")"})
	}
	return opts
}
