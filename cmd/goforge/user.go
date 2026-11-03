package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/database"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/logger"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/services"
)

var userCmd = &cobra.Command{
	Use:   "user",
	Short: "Account maintenance from the host",
}

var userReset2FACmd = &cobra.Command{
	Use:   "reset-2fa <email>",
	Short: "Turn off two-factor authentication for an account (lockout recovery)",
	Long: `Turn off TOTP two-factor authentication for the account with this email,
for someone who lost both their authenticator and their recovery codes.

It deletes the account's TOTP secret, its recovery codes, and any pending
sign-in step, and writes a user.2fa.reset audit row (via cli). The password,
sessions, and API tokens are not changed. The user can turn 2FA on again in
Settings → Profile.

Run it where GoForge's database settings are, for example:

  docker compose exec goforge ./goforge user reset-2fa you@example.com`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runUserReset2FA(cmd.Context(), cmd.OutOrStdout(), args[0])
	},
}

func init() {
	rootCmd.AddCommand(userCmd)
	userCmd.AddCommand(userReset2FACmd)
}

func runUserReset2FA(ctx context.Context, out io.Writer, email string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return errors.New("email is required")
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	log := logger.Setup(cfg.LogLevel, cfg.LogFormat)
	db, err := database.New(cfg) //nolint:contextcheck // database.New takes no context
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	// Reset only deletes rows, so it needs no ENCRYPTION_KEY.
	users := repositories.NewUserRepository(db.DB, nil)
	audit := services.NewAuditService(repositories.NewAuditLogRepository(db.DB), logger.WithComponent(log, "audit"))
	return reset2FA(ctx, out, email, users, auth.NewTwoFactor(repositories.NewTwoFactorRepository(db.DB, nil)), audit)
}

type userByEmail interface {
	GetByEmail(ctx context.Context, email string) (*models.User, error)
}

type syncAuditRecorder interface {
	RecordAs(ctx context.Context, id, userID, action, resourceType, resourceID string, details map[string]interface{}, ip, userAgent string)
}

func reset2FA(ctx context.Context, out io.Writer, email string, users userByEmail, tf *auth.TwoFactor, audit syncAuditRecorder) error {
	user, err := users.GetByEmail(ctx, email)
	if errors.Is(err, repositories.ErrNotFound) {
		return fmt.Errorf("no account with email %q", email)
	}
	if err != nil {
		return fmt.Errorf("failed to look up account: %w", err)
	}
	removed, err := tf.Reset(ctx, user.ID)
	if err != nil {
		return fmt.Errorf("failed to reset two-factor authentication (has migration 030 run? try goforge migrate up): %w", err)
	}
	if !removed {
		_, _ = fmt.Fprintf(out, "%s does not have two-factor authentication on; nothing changed.\n", user.Email)
		return nil
	}
	// RecordAs writes before returning, so the row lands before the process exits.
	audit.RecordAs(ctx, "", user.ID, models.AuditActionUser2FAReset, models.ResourceTypeUser, user.ID,
		map[string]interface{}{"via": models.AuditViaCLI}, "", "goforge-cli")
	_, _ = fmt.Fprintf(out, "Two-factor authentication is off for %s. They can sign in with their password and turn it on again in Settings → Profile.\n", user.Email)
	return nil
}
