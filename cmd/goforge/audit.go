package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/raythurman2386/goforge/internal/audit"
	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/database"
	"github.com/raythurman2386/goforge/internal/logger"
)

// auditFromSeq is `goforge audit verify --from-seq`. 0 checks the whole chain.
var auditFromSeq int64

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Audit log integrity",
}

var auditVerifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify the audit log hash chain",
	Long: `Verify the HMAC-SHA256 chain over audit_logs.

Exits 0 when the chain is intact and prints the head seq and hash.
Exits 1 and names the first bad seq when a row was changed, deleted, or
left unsealed, or when the retention boundary does not match.

The key is derived from ENCRYPTION_KEY. This does not stop someone who can
read that key (the Docker socket or root on the host). Rows sealed after an
upgrade only prove the log is unchanged since that upgrade.

--from-seq N starts at that seal instead of the oldest one.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if auditFromSeq < 0 {
			err := fmt.Errorf("--from-seq must be >= 0")
			return writeErr(cmd.ErrOrStderr(), err)
		}
		return runAuditVerify(cmd, auditFromSeq)
	},
}

func writeErr(w io.Writer, err error) error {
	if _, werr := fmt.Fprintln(w, err.Error()); werr != nil {
		return werr
	}
	return err
}

func writeLine(w io.Writer, line string) error {
	_, err := fmt.Fprintln(w, line)
	return err
}

func runAuditVerify(cmd *cobra.Command, fromSeq int64) error {
	cfg, err := config.Load()
	if err != nil {
		return writeErr(cmd.ErrOrStderr(), err)
	}
	log := logger.Setup(cfg.LogLevel, cfg.LogFormat)
	db, err := database.New(cfg)
	if err != nil {
		return writeErr(cmd.ErrOrStderr(), fmt.Errorf("failed to connect to database: %w", err))
	}
	defer db.Close()

	key, err := audit.DeriveChainKey(cfg.EncryptionKey)
	if err != nil {
		return writeErr(cmd.ErrOrStderr(), err)
	}
	chain := audit.NewChain(db.DB, key, logger.WithComponent(log, "audit"))
	seq, hash, fail, err := chain.Verify(cmd.Context(), fromSeq)
	out := cmd.OutOrStdout()
	switch {
	case err != nil:
		return writeErr(cmd.ErrOrStderr(), err)
	case fail != nil:
		if werr := writeLine(out, fail.Error()); werr != nil {
			return werr
		}
		return fail
	default:
		return writeLine(out, audit.FormatVerified(seq, hash))
	}
}

func init() {
	auditVerifyCmd.Flags().Int64Var(&auditFromSeq, "from-seq", 0, "start verification at this seal seq (0 checks from the oldest)")
	auditCmd.AddCommand(auditVerifyCmd)
	rootCmd.AddCommand(auditCmd)
}
