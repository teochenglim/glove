//go:build !race

package cli

// See timing_race.go: no instrumentation, real budgets apply (PRD §8).
const timingBudgetScale = 1
