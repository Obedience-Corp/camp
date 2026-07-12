package ledgerkit

import (
	"context"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/google/uuid"
)

// ResolveWriterID returns this machine's stable ledger writer slug, generating
// and persisting one on first use. The slug lives in the machine-local global
// config (~/.obey/campaign/config.json), never in a committed campaign, so two
// machines writing the same campaign use different shard files and the ledger
// stays merge-conflict-free (D002).
//
// It is the single resolution path shared by camp and fest (F5): both import
// ledgerkit, so both agree on the same machine slug. The core Writer takes the
// id explicitly (dependency injection); this resolver is the only piece coupled
// to camp's config.
func ResolveWriterID(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	cfg, err := config.LoadGlobalConfig(ctx)
	if err != nil {
		return "", camperrors.Wrap(err, "ledgerkit: load global config for writer id")
	}
	if cfg.LedgerWriterID != "" {
		return cfg.LedgerWriterID, nil
	}
	cfg.LedgerWriterID = generateWriterID()
	if err := config.SaveGlobalConfig(ctx, cfg); err != nil {
		return "", camperrors.Wrap(err, "ledgerkit: persist generated writer id")
	}
	return cfg.LedgerWriterID, nil
}

// generateWriterID returns a short, filesystem-safe, collision-resistant slug
// for a shard filename. The leading "w" keeps it a valid identifier and marks
// it as a writer slug.
func generateWriterID() string {
	return "w" + uuid.NewString()[:8]
}
