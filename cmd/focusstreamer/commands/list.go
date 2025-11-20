package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/bryanchriswhite/FocusStreamer/internal/config"
	"github.com/bryanchriswhite/FocusStreamer/internal/window"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List running applications",
	Long: `List all running applications detected by FocusStreamer.

This command connects to the X11 server and retrieves information about
all currently running applications and their windows.`,
	Example: `  # List applications in table format (default)
  focusstreamer list

  # List applications in JSON format
  focusstreamer list --format json

  # List only whitelisted applications
  focusstreamer list --whitelisted

  # List the currently focused window
  focusstreamer list --current`,
	RunE: runList,
}

var (
	listFormat      string
	listWhitelisted bool
	listCurrent     bool
)

func init() {
	rootCmd.AddCommand(listCmd)

	listCmd.Flags().StringVarP(&listFormat, "format", "f", "table", "output format (table or json)")
	listCmd.Flags().BoolVarP(&listWhitelisted, "whitelisted", "w", false, "show only whitelisted applications")
	listCmd.Flags().BoolVarP(&listCurrent, "current", "c", false, "show current focused window")
}

func runList(cmd *cobra.Command, args []string) error {
	// Initialize configuration
	configMgr, err := config.NewManager(GetConfigFile())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize window manager
	windowMgr, err := window.NewManager(configMgr)
	if err != nil {
		return fmt.Errorf("failed to connect to X11: %w", err)
	}
	defer windowMgr.Stop()

	// Show current window if requested
	if listCurrent {
		return showCurrentWindow(windowMgr)
	}

	// Get applications
	apps, err := windowMgr.GetApplications()
	if err != nil {
		return fmt.Errorf("failed to get applications: %w", err)
	}

	// Filter whitelisted if requested
	if listWhitelisted {
		filtered := make([]config.Application, 0)
		for _, app := range apps {
			if app.Whitelisted {
				filtered = append(filtered, app)
			}
		}
		apps = filtered
	}

	// Output in requested format
	switch listFormat {
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(apps)
	case "table":
		return printAppsTable(apps)
	default:
		return fmt.Errorf("unsupported format: %s (use 'table' or 'json')", listFormat)
	}
}

func printAppsTable(apps []config.Application) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()

	fmt.Fprintln(w, "NAME\tCLASS\tPID\tWHITELISTED")
	fmt.Fprintln(w, "----\t-----\t---\t-----------")

	for _, app := range apps {
		whitelisted := "No"
		if app.Whitelisted {
			whitelisted = "Yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", app.Name, app.WindowClass, app.PID, whitelisted)
	}

	return nil
}

func showCurrentWindow(windowMgr *window.Manager) error {
	current := windowMgr.GetCurrentWindow()
	if current == nil {
		fmt.Println("No window is currently focused")
		return nil
	}

	if listFormat == "json" {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(current)
	}

	fmt.Printf("Title:    %s\n", current.Title)
	fmt.Printf("Class:    %s\n", current.Class)
	fmt.Printf("PID:      %d\n", current.PID)
	fmt.Printf("Geometry: %dx%d at (%d, %d)\n",
		current.Geometry.Width, current.Geometry.Height,
		current.Geometry.X, current.Geometry.Y)

	return nil
}
