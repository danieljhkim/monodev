package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
)

var (
	// Color functions - will be nil if output is not a TTY
	successColor = color.New(color.FgGreen, color.Bold)
	warningColor = color.New(color.FgYellow, color.Bold)
	errorColor   = color.New(color.FgRed, color.Bold)
	infoColor    = color.New(color.FgCyan)
	headerColor  = color.New(color.FgBlue, color.Bold)
	labelColor   = color.New(color.FgWhite, color.Bold)
	valueColor   = color.New(color.FgHiBlack)
	dimColor     = color.New(color.FgHiBlack)
)

// initColors initializes color output - fatih/color handles TTY detection automatically
// This is a no-op but kept for potential future initialization needs
func initColors() {
	// fatih/color automatically detects TTY and disables colors when needed
	// No explicit initialization required
}

// PrintSection prints a section header
func PrintSection(title string) {
	initColors()
	fmt.Println()
	_, _ = headerColor.Printf("▸ %s\n", title)
	fmt.Println()
}

// PrintSubsection prints a subsection header
func PrintSubsection(title string) {
	initColors()
	_, _ = infoColor.Printf("  %s\n", title)
}

// PrintSuccess prints a success message with a checkmark
func PrintSuccess(msg string) {
	initColors()
	_, _ = successColor.Printf("✓ %s\n", msg)
}

// PrintWarning prints a warning message with a warning symbol
func PrintWarning(msg string) {
	initColors()
	_, _ = warningColor.Printf("⚠ %s\n", msg)
}

// PrintError prints an error message to stderr
func PrintError(msg string) {
	initColors()
	_, _ = errorColor.Fprintf(os.Stderr, "✗ %s\n", msg)
}

// PrintInfo prints an informational message
func PrintInfo(msg string) {
	initColors()
	fmt.Println(msg)
}

// PrintLabelValue prints a label-value pair with proper formatting
func PrintLabelValue(label, value string) {
	initColors()
	_, _ = labelColor.Printf("  %s: ", label)
	_, _ = valueColor.Println(value)
}

// PrintLabelValueWithColor prints a label-value pair with a custom value color
func PrintLabelValueWithColor(label, value string, valueClr *color.Color) {
	initColors()
	_, _ = labelColor.Printf("  %s: ", label)
	_, _ = valueClr.Println(value)
}

// PrintList prints a list of items with bullet points
func PrintList(items []string, indent int) {
	initColors()
	indentStr := strings.Repeat("  ", indent)
	for _, item := range items {
		_, _ = infoColor.Printf("%s• %s\n", indentStr, item)
	}
}

// PrintNumberedList prints a numbered list
func PrintNumberedList(items []string, indent int) {
	initColors()
	indentStr := strings.Repeat("  ", indent)
	for i, item := range items {
		_, _ = infoColor.Printf("%s%d. %s\n", indentStr, i+1, item)
	}
}

// PrintTable prints a simple two-column table
func PrintTable(headers []string, rows [][]string) {
	initColors()
	if len(headers) == 0 || len(rows) == 0 {
		return
	}

	// Calculate column widths
	colWidths := make([]int, len(headers))
	for i, header := range headers {
		colWidths[i] = len(header)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(colWidths) && len(cell) > colWidths[i] {
				colWidths[i] = len(cell)
			}
		}
	}

	// Print header
	_, _ = headerColor.Print("  ")
	for i, header := range headers {
		if i > 0 {
			fmt.Print("  ")
		}
		_, _ = headerColor.Printf("%-*s", colWidths[i], header)
	}
	fmt.Println()

	// Print separator
	fmt.Print("  ")
	for i, width := range colWidths {
		if i > 0 {
			fmt.Print("  ")
		}
		fmt.Print(strings.Repeat("-", width))
	}
	fmt.Println()

	// Print rows
	for _, row := range rows {
		fmt.Print("  ")
		for i, cell := range row {
			if i >= len(colWidths) {
				break
			}
			if i > 0 {
				fmt.Print("  ")
			}
			_, _ = valueColor.Printf("%-*s", colWidths[i], cell)
		}
		fmt.Println()
	}
}

// PrintEmptyState prints a message when there's no data to show
func PrintEmptyState(msg string) {
	initColors()
	_, _ = dimColor.Printf("  %s\n", msg)
}

// PrintBadge prints a colored badge/tag
func PrintBadge(text string, clr *color.Color) {
	initColors()
	_, _ = clr.Printf("  [%s]", text)
}

// PrintSeparator prints a visual separator line
func PrintSeparator() {
	initColors()
	_, _ = labelColor.Println("\n  ──────────────────────────────────────────────────────────")
}

// PrintCount prints a count with proper formatting
func PrintCount(count int, singular, plural string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, singular)
	}
	return fmt.Sprintf("%d %s", count, plural)
}
