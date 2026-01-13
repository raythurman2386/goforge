package auth

import (
	"context"
	"fmt"

	"github.com/google/go-github/v53/github"
	"golang.org/x/oauth2"
	githuboauth "golang.org/x/oauth2/github"

	"github.com/raythurman2386/gocool/internal/config"
)

// GitHubProvider handles GitHub OAuth.
type GitHubProvider struct {
	oauthConfig *oauth2.Config
}

// NewGitHubProvider creates a new GitHubProvider.
func NewGitHubProvider(cfg *config.Config) *GitHubProvider {
	return &GitHubProvider{
		oauthConfig: &oauth2.Config{
			ClientID:     cfg.GitHubClientID,
			ClientSecret: cfg.GitHubClientSecret,
			Endpoint:     githuboauth.Endpoint,
			Scopes:       []string{"user:email", "repo"},
			RedirectURL:  fmt.Sprintf("%s/auth/github/callback", cfg.BaseURL),
		},
	}
}

// AuthURL returns the URL to redirect the user to for authentication.
func (p *GitHubProvider) AuthURL(state string) string {
	return p.oauthConfig.AuthCodeURL(state)
}

// Exchange exchanges an authorization code for a token.
func (p *GitHubProvider) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return p.oauthConfig.Exchange(ctx, code)
}

// GetUser retrieves the GitHub user info.
func (p *GitHubProvider) GetUser(ctx context.Context, token *oauth2.Token) (*github.User, error) {
	ts := oauth2.StaticTokenSource(token)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	user, _, err := client.Users.Get(ctx, "")
	return user, err
}
