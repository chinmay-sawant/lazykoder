package subagent

import (
	"strings"
	"time"
)

const (
	// HardMaxConcurrent is the absolute concurrency ceiling.
	HardMaxConcurrent = 20
	// DefaultMaxConcurrent is the default slot count.
	DefaultMaxConcurrent = 4
	// DefaultMaxQueued is the default cap on running+queued jobs.
	DefaultMaxQueued = 40
	// DefaultMaxDepth is 1 (no nested task tools on children).
	DefaultMaxDepth = 1
	// DefaultTimeout is the child wall-clock timeout.
	DefaultTimeout = 600 * time.Second
	// DefaultChildMaxSteps is the child agent step budget.
	// Keep in sync with settings.DefaultChildMaxSteps.
	DefaultChildMaxSteps = 32
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
	Endpoint             string
	Variant              string
	ExploreModel         string
	BashConfirm          string // parent|deny
	AllowParallelWriters bool
	DefaultRole          string
}

// NewConfig returns production defaults with sub-agents enabled.
func NewConfig() Config {
	return Config{
		Enabled:              true,
		MaxConcurrent:        DefaultMaxConcurrent,
		MaxQueued:            DefaultMaxQueued,
		MaxDepth:             DefaultMaxDepth,
		Timeout:              DefaultTimeout,
		ChildMaxSteps:        DefaultChildMaxSteps,
		BashConfirm:          BashConfirmParent,
		AllowParallelWriters: false,
		DefaultRole:          DefaultRole,
	}.Normalize()
}

// Normalize clamps fields to safe ranges and fills defaults.
func (c Config) Normalize() Config {
	if c.MaxConcurrent < 1 {
		c.MaxConcurrent = DefaultMaxConcurrent
	}
	if c.MaxConcurrent > HardMaxConcurrent {
		c.MaxConcurrent = HardMaxConcurrent
	}
	if c.MaxQueued < 1 {
		c.MaxQueued = DefaultMaxQueued
	}
	if c.MaxDepth < 1 {
		c.MaxDepth = DefaultMaxDepth
	}
	if c.Timeout <= 0 {
		c.Timeout = DefaultTimeout
	}
	if c.ChildMaxSteps < 1 {
		c.ChildMaxSteps = DefaultChildMaxSteps
	}
	c.Model = strings.TrimSpace(c.Model)
	c.Endpoint = strings.TrimSpace(c.Endpoint)
	c.Variant = strings.TrimSpace(c.Variant)
	c.ExploreModel = strings.TrimSpace(c.ExploreModel)
	c.BashConfirm = strings.TrimSpace(strings.ToLower(c.BashConfirm))
	if c.BashConfirm != BashConfirmParent && c.BashConfirm != BashConfirmDeny {
		c.BashConfirm = BashConfirmParent
	}
	c.DefaultRole = normalizeRole(c.DefaultRole, DefaultRole)
	return c
}

func normalizeRole(role, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case RoleExplore:
		return RoleExplore
	case RolePlan:
		return RolePlan
	case RoleGeneral:
		return RoleGeneral
	default:
		if fallback == "" {
			return DefaultRole
		}
		return normalizeRole(fallback, DefaultRole)
	}
}
