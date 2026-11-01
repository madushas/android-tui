package ui

import (
	"os"

	"github.com/charmbracelet/lipgloss"
)

// ApplyTheme switches the style variables: "default" follows the terminal's 16-colour palette, "contrast" uses the bright
// variants and a readable grey, "mono" drops colour and uses only bold/underline/faint/reverse. Call before the UI starts.
func ApplyTheme(name string) {
	grey, ok, warn, errc, acc, blue, mag := "8", "2", "3", "1", "6", "4", "5"
	if name == "contrast" {
		grey, ok, warn, errc, acc, blue, mag = "7", "10", "11", "9", "14", "12", "13"
	}
	fg := func(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }
	if name == "mono" {
		sOK, sWarn, sErr = sNone, sNone.Bold(true), sNone.Bold(true).Underline(true)
		sAcc, sBlue, sMag = sNone, sNone, sNone
		sDim = sNone.Faint(true)
		sSel = sNone.Reverse(true).Bold(true)
	} else {
		sOK, sWarn, sErr = fg(ok), fg(warn), fg(errc)
		sAcc, sBlue, sMag = fg(acc), fg(blue), fg(mag)
		sDim = fg(grey)
		sSel = lipgloss.NewStyle().Background(lipgloss.Color(blue)).Foreground(lipgloss.Color("15")).Bold(true)
	}
	sOKB, sWarnB, sErrB, sAccB = sOK.Bold(true), sWarn.Bold(true), sErr.Bold(true), sAcc.Bold(true)
	if name == "mono" {
		sAccB = sNone.Bold(true).Underline(true)
	}
	styleByName = map[string]lipgloss.Style{"": sNone, "dim": sDim, "ok": sOK, "okb": sOKB, "err": sErr, "errb": sErrB, "warn": sWarn,
		"acc": sAcc, "accb": sAccB, "bold": sBold, "blue": sBlue, "mag": sMag}
}

// themeFromEnv resolves the theme: ANDROID_TUI_THEME, then NO_COLOR (mono), then the THEME config value.
func themeFromEnv(cfg string) string {
	if t := os.Getenv("ANDROID_TUI_THEME"); t != "" {
		return t
	}
	if os.Getenv("NO_COLOR") != "" {
		return "mono"
	}
	return cfg
}
