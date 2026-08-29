package subagent

import (
	"strings"
	"time"

	"github.com/chinmay-sawant/lazykoder/internal/roles"
	"github.com/chinmay-sawant/lazykoder/internal/settings"
)

const (
	// DefaultRole is used when Spec.Role is empty.
	DefaultRole = RoleExplore
	// BashConfirmParent asks the parent UI to confirm child bash.
	BashConfirmParent = "parent"
	// BashConfirmDeny refuses child bash that would ask.
	BashConfirmDeny = "deny"
)

// Config controls Manager limits and child defaults.
type Config struct {
	Enabled              bool
	MaxConcurrent        int // clamped 1..HardMaxConcurrent
	MaxQueued            int
	MaxDepth             int // 1 = no nested spawn
	Timeout              time.Duration
	ChildMaxSteps        int
	Model                string // inherit if empty (runner)
	Variant              string
	ExploreModel         string
	ExploreClass         string
	PlanClass            string
	GeneralClass         string
	BashConfirm          string // parent|deny
	AllowParallelWriters bool
	DefaultRole          string
}

// NewConfig returns production defaults with sub-agents enabled.
func NewConfig() Config {
	return ConfigFromSettings(settings.Default())
}

// Normalize clamps fields to safe ranges and fills defaults.
func (c Config) Normalize() Config {
	if c.MaxConcurrent < settings.MinMaxConcurrent {
		c.MaxConcurrent = settings.DefaultMaxConcurrent
	}
	if c.MaxConcurrent > settings.MaxMaxConcurrent {
		c.MaxConcurrent = settings.MaxMaxConcurrent
	}
	if c.MaxQueued < 1 {
		c.MaxQueued = settings.DefaultMaxQueued
	}
	if c.MaxQueued > settings.MaxMaxQueued {
		c.MaxQueued = settings.MaxMaxQueued
	}
	if c.MaxQueued < c.MaxConcurrent {
		c.MaxQueued = c.MaxConcurrent
	}
	if c.MaxDepth < settings.DefaultMaxDepth {
		c.MaxDepth = settings.DefaultMaxDepth
	}
	if c.MaxDepth > settings.MaxMaxDepth {
		c.MaxDepth = settings.MaxMaxDepth
	}
	if c.Timeout < 0 {
		c.Timeout = time.Duration(settings.DefaultAgentsTimeoutSec) * time.Second
	}
	if c.ChildMaxSteps < settings.MinMaxSteps {
		c.ChildMaxSteps = settings.DefaultChildMaxSteps
	}
	if c.ChildMaxSteps > settings.MaxMaxSteps {
		c.ChildMaxSteps = settings.MaxMaxSteps
	}
	c.Model = strings.TrimSpace(c.Model)
	c.Variant = strings.TrimSpace(c.Variant)
	c.ExploreModel = strings.TrimSpace(c.ExploreModel)
	c.ExploreClass = strings.TrimSpace(strings.ToLower(c.ExploreClass))
	c.PlanClass = strings.TrimSpace(strings.ToLower(c.PlanClass))
	c.GeneralClass = strings.TrimSpace(strings.ToLower(c.GeneralClass))
	c.BashConfirm = strings.TrimSpace(strings.ToLower(c.BashConfirm))
	if c.BashConfirm != BashConfirmParent && c.BashConfirm != BashConfirmDeny {
		c.BashConfirm = BashConfirmParent
	}
	c.DefaultRole = normalizeRole(c.DefaultRole, DefaultRole)
	return c
}

func normalizeRole(role, fallback string) string {
	return roles.Normalize(role, fallback)
}
