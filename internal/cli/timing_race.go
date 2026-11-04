//go:build race

package cli

// timingBudgetScale inflates absolute timing budgets under the race
// detector — instrumentation slows the binary by roughly an order of
// magnitude, so wall-clock PRD budgets are meaningless in `-race` runs.
// The un-instrumented `make check` gates keep the real budgets (PRD §8).
const timingBudgetScale = 10
