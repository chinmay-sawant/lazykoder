package subagent

import (
	"time"

	"github.com/chinmay-sawant/lazykoder/internal/roles"
	"github.com/chinmay-sawant/lazykoder/internal/settings"
)

// ConfigFromSettings maps project settings into a Manager Config.
func ConfigFromSettings(s settings.Settings) Config {
	a := s.EffectiveAgents()
	cfg := Config{
		Enabled:              a.Enabled,
		MaxConcurrent:        a.MaxConcurrent,
		MaxQueued:            a.MaxQueued,
		MaxDepth:             a.MaxDepth,
		ChildMaxSteps:        a.ChildMaxSteps,
		Model:                a.ModelOverride,
		Variant:              a.ModelVariant,
		ExploreModel:         a.ExploreModel,
		ModelByRole:          map[string]string{roles.Explore: a.ExploreModel},
		ModelClassByRole:     s.EffectiveOrchestrator().ModelClassByRole,
		Roles:                roles.Roles(),
		ExploreClass:         s.EffectiveOrchestrator().ExploreClass,
		PlanClass:            s.EffectiveOrchestrator().PlanClass,
		GeneralClass:         s.EffectiveOrchestrator().GeneralClass,
		BashConfirm:          a.BashConfirm,
		AllowParallelWriters: a.AllowParallelWriters,
		DefaultRole:          a.DefaultRole,
	}
	if a.DefaultTimeoutSec > 0 {
		cfg.Timeout = time.Duration(a.DefaultTimeoutSec) * time.Second
	}
	return cfg.Normalize()
}
