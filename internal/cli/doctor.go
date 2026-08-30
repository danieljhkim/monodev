package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/engine"
)

var doctorFix bool

var errDoctorProblemsFound = errors.New("doctor found unresolved problems")

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose and repair inconsistent local monodev state",
	Long: `Check monodev's on-disk state for drift and interrupted transactions:
pending overlay transaction journals, orphaned backup directories, ledger
entries owned by a deleted store, workspaces whose checkout no longer
exists, stale lock files, remote persistence misconfiguration, and drift
between .git/info/exclude and the workspace ledger.

Read-only by default. Pass --fix to apply the safe repairs: complete or
roll back a pending transaction, prune ledger entries for stores that no
longer exist, remove orphaned backup directories, and reconcile the
managed exclude block. Findings that could destroy user content are
reported only.

Exits non-zero when unresolved problems remain, so it can be used in
scripts and CI.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := newEngine()
		if err != nil {
			return err
		}

		ctx := context.Background()
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		result, err := eng.Doctor(ctx, &engine.DoctorRequest{CWD: cwd, Fix: doctorFix})
		if err != nil {
			return err
		}

		if jsonOutput {
			if outputErr := outputJSON(result); outputErr != nil {
				return outputErr
			}
			if !result.Healthy() {
				return errDoctorProblemsFound
			}
			return nil
		}

		printDoctorResult(result)

		if !result.Healthy() {
			return errDoctorProblemsFound
		}
		return nil
	},
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorFix, "fix", false, "Apply the safe repairs for fixable findings")
}

func printDoctorResult(result *engine.DoctorResult) {
	PrintSection("Doctor")

	if len(result.Findings) == 0 {
		PrintSuccess("Everything looks good — no drift or interrupted transactions found")
		return
	}

	problems := 0
	fixed := 0
	for _, finding := range result.Findings {
		switch finding.Severity {
		case engine.DoctorSeverityInfo:
			PrintInfo(fmt.Sprintf("• %s", finding.Message))
			continue
		}

		if finding.Fixed {
			fixed++
			PrintSuccess(finding.Message)
			continue
		}

		problems++
		PrintWarning(finding.Message)
		if finding.Recovery != "" {
			PrintLabelValue("Recovery", finding.Recovery)
		}
		if finding.FixError != "" {
			PrintError(fmt.Sprintf("fix failed: %s", finding.FixError))
		}
	}

	fmt.Println()
	if result.Healthy() {
		PrintSuccess(fmt.Sprintf("Fixed %s; no unresolved problems remain", PrintCount(fixed, "issue", "issues")))
		return
	}
	if result.Fixed {
		PrintWarning(fmt.Sprintf("%s could not be fixed automatically; see recovery notes above", PrintCount(problems, "problem", "problems")))
	} else {
		PrintWarning(fmt.Sprintf("Found %s; run `monodev doctor --fix` to repair what can be repaired automatically", PrintCount(problems, "problem", "problems")))
	}
}
