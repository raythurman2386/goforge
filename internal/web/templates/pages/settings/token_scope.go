package settings

import "github.com/raythurman2386/goforge/internal/models"

// TokenResource is a project or service the token form offers, or a
// token row names.
type TokenResource struct {
	ID   string
	Name string
}

// TokenResources are the owner's projects and services: the choices on the
// token form and the names on the token list. A nil *TokenResources hides
// the form's restriction section.
type TokenResources struct {
	Projects []TokenResource
	Services []TokenResource
}

func (t *TokenResources) hasAny() bool {
	return t != nil && (len(t.Projects) > 0 || len(t.Services) > 0)
}

func (t *TokenResources) name(list []TokenResource, id string) string {
	if t != nil {
		for _, r := range list {
			if r.ID == id {
				return r.Name
			}
		}
	}
	short := id
	if len(short) > 8 {
		short = short[:8]
	}
	return short + " (deleted)"
}

// tokenRestriction is one badge on a restricted token row.
type tokenRestriction struct {
	Kind, Name, ID string
}

// tokenRestrictions lists a restricted token's projects, then services, by
// name. A host-wide token has none.
func tokenRestrictions(token *models.APIToken, res *TokenResources) []tokenRestriction {
	out := []tokenRestriction{}
	var projects, services []TokenResource
	if res != nil {
		projects, services = res.Projects, res.Services
	}
	for _, id := range token.ProjectIDs {
		out = append(out, tokenRestriction{Kind: "project", ID: id, Name: res.name(projects, id)})
	}
	for _, id := range token.ServiceIDs {
		out = append(out, tokenRestriction{Kind: "service", ID: id, Name: res.name(services, id)})
	}
	return out
}
