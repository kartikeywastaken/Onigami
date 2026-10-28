package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// This file holds small helpers for reading optional, operator-tunable knobs
// from the environment. The pattern is always the same: a sensible default
// baked into the call site, overridable via an env var, and never a bare magic
// number sitting inline. Timeouts, retry counts, and backoff all flow through
// here so they can be tuned (e.g. in .env) without touching code.

// IntEnv returns the integer value of env var `name`, or `def` if the variable
// is unset, blank, or not a non-negative integer.
func IntEnv(name string, def int) int {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return def
}

// SecondsEnv reads env var `name` as a number of seconds (e.g. "10" or "2.5")
// and returns it as a time.Duration, or `def` if unset, blank, or not a
// positive number.
func SecondsEnv(name string, def time.Duration) time.Duration {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return time.Duration(f * float64(time.Second))
		}
	}
	return def
}
