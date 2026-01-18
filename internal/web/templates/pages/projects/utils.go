// Package projects contains templates for project pages.
package projects

import (
	"fmt"

	"github.com/raythurman2386/goforge/internal/models"
)

func getPort(env *models.Environment) string {
	if env.Port.Valid {
		return fmt.Sprintf("%d", env.Port.Int32)
	}
	return "80"
}
