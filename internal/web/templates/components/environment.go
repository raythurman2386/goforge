package components

import (
	"github.com/a-h/templ"

	"github.com/raythurman2386/goforge/internal/models"
)

// currentEnvName is the switcher label: the current environment, else the
// first one (the project page defaults to it), else "No Environments".
func currentEnvName(envs []*models.Environment, currentEnvID string) string {
	for _, e := range envs {
		if e.ID == currentEnvID {
			return e.Name
		}
	}
	if len(envs) > 0 {
		return envs[0].Name
	}
	return "No Environments"
}

// envItemAttrs marks the menu link of the environment on screen.
func envItemAttrs(current bool) templ.Attributes {
	if current {
		return templ.Attributes{"aria-current": "page"}
	}
	return nil
}
