package intent

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/intent"
)

const (
	flagPriority          = "priority"
	flagHorizon           = "horizon"
	flagBlockedBy         = "blocked-by"
	flagDependsOn         = "depends-on"
	flagPromotionCriteria = "promotion-criteria"
)

var addMetadataFlags = []string{flagPriority, flagHorizon, flagBlockedBy, flagDependsOn, flagPromotionCriteria}

type addMetadata struct {
	priority          intent.Priority
	horizon           intent.Horizon
	blockedBy         []string
	dependsOn         []string
	promotionCriteria string
}

func registerAddMetadataFlags(flags *pflag.FlagSet) {
	flags.String(flagPriority, "", "Set priority (low, medium, high; default medium)")
	flags.String(flagHorizon, "", "Set horizon (now, next, later, someday; default later)")
	flags.StringArray(flagBlockedBy, nil, "Idea or festival ID this is blocked by (repeatable)")
	flags.StringArray(flagDependsOn, nil, "Idea or festival ID this depends on (repeatable)")
	flags.String(flagPromotionCriteria, "", "What must be true before this can become a festival")
}

func resolveAddMetadata(cmd *cobra.Command, createNote bool) (addMetadata, error) {
	var meta addMetadata
	flags := cmd.Flags()

	if createNote {
		for _, name := range addMetadataFlags {
			if flags.Changed(name) {
				return meta, camperrors.Wrapf(camperrors.ErrInvalidInput, "--note cannot be combined with --%s", name)
			}
		}
		return meta, nil
	}

	if flags.Changed(flagPriority) {
		value, _ := flags.GetString(flagPriority)
		priority, err := intent.ParsePriority(value)
		if err != nil {
			return meta, camperrors.Wrap(err, "--"+flagPriority)
		}
		meta.priority = priority
	}
	if flags.Changed(flagHorizon) {
		value, _ := flags.GetString(flagHorizon)
		horizon, err := intent.ParseHorizon(value)
		if err != nil {
			return meta, camperrors.Wrap(err, "--"+flagHorizon)
		}
		meta.horizon = horizon
	}

	refs, err := intent.NormalizeDependencyRefs("blocked_by", stringArrayFlag(flags, flagBlockedBy))
	if err != nil {
		return meta, camperrors.Wrap(err, "--"+flagBlockedBy)
	}
	meta.blockedBy = refs

	refs, err = intent.NormalizeDependencyRefs("depends_on", stringArrayFlag(flags, flagDependsOn))
	if err != nil {
		return meta, camperrors.Wrap(err, "--"+flagDependsOn)
	}
	meta.dependsOn = refs

	criteria, _ := flags.GetString(flagPromotionCriteria)
	meta.promotionCriteria = strings.TrimSpace(criteria)

	return meta, nil
}

// stringArrayFlag returns the values given for a repeatable flag.
// FlagSet.GetStringArray round-trips through CSV and drops a lone empty
// value, which would hide `--blocked-by ""` from validation.
func stringArrayFlag(flags *pflag.FlagSet, name string) []string {
	flag := flags.Lookup(name)
	if flag == nil || !flag.Changed {
		return nil
	}
	values, ok := flag.Value.(pflag.SliceValue)
	if !ok {
		return nil
	}
	return values.GetSlice()
}

func (m addMetadata) apply(opts *intent.CreateOptions) {
	opts.Priority = m.priority
	opts.Horizon = m.horizon
	opts.BlockedBy = m.blockedBy
	opts.DependsOn = m.dependsOn
	opts.PromotionCriteria = m.promotionCriteria
}
