package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/danieljhkim/monodev/internal/engine"
)

var trackCmd = &cobra.Command{
	Use:   "track <path>...",
	Short: "Track paths in the active store",
	Long:  `Add paths to the active store's track file.`,
	Args:  cobra.MinimumNArgs(1),
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

		req := &engine.TrackRequest{
			CWD:   cwd,
			Paths: args,
		}

		if err := eng.Track(ctx, req); err != nil {
			return err
		}

		fmt.Printf("Tracked %d paths\n", len(args))
		return nil
	},
}
