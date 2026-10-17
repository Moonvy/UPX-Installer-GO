package main

import "github.com/egoist/mygo/ui"

// The app's palette: a modern dark interface with a refined violet accent.
var (
	colBackground    = ui.Hex("#0e1015")
	colSurface       = ui.Hex("#161820")
	colSurfaceHover  = ui.Hex("#1e212b")
	colBorder        = ui.RGBA(0xff, 0xff, 0xff, 0.10)
	colBorderSubtle  = ui.RGBA(0xff, 0xff, 0xff, 0.06)
	colText          = ui.Hex("#f4f5f8")
	colTextMuted     = ui.Hex("#9ea2b0")
	colTextFaint     = ui.Hex("#727686")
	colAccent        = ui.Hex("#856eff")
	colAccentHover   = ui.Hex("#9d8bff")
	colAccentPressed = ui.Hex("#6d55ea")
	colAccentText    = ui.Hex("#ffffff")
	colAccentSoft    = ui.RGBA(0x85, 0x6e, 0xff, 0.15)
	colAccentPanel   = ui.RGBA(0x5a, 0x47, 0xb0, 0.22)
	colAccentLine    = ui.RGBA(0x85, 0x6e, 0xff, 0.45)
	colSuccess       = ui.Hex("#4ade80")
	colSuccessSoft   = ui.RGBA(0x22, 0xc5, 0x5e, 0.12)
	colSuccessLine   = ui.RGBA(0x22, 0xc5, 0x5e, 0.28)
	colWarning       = ui.Hex("#fbbf24")
	colWarningSoft   = ui.RGBA(0xf5, 0x9e, 0x0b, 0.12)
	colWarningLine   = ui.RGBA(0xf5, 0x9e, 0x0b, 0.28)
	colDanger        = ui.Hex("#f87171")
	colDangerSoft    = ui.RGBA(0xef, 0x44, 0x44, 0.12)
	colDangerLine    = ui.RGBA(0xef, 0x44, 0x44, 0.28)

	// Tags
	colTagHostBg   = ui.RGBA(0xff, 0xff, 0xff, 0.07)
	colTagHostText = ui.Hex("#b8bcc9")
	colCEPBg       = ui.RGBA(0xf5, 0x9e, 0x0b, 0.15)
	colCEPText     = ui.Hex("#fbbf24")
	colUXPBg       = ui.RGBA(0x85, 0x6e, 0xff, 0.15)
	colUXPText     = ui.Hex("#a797ff")
)

// The surfaces are translucent so that the window's material shows through
// them, most of all in the gaps between the panels, which draw only the
// scrim. Where a window has none, the root draws the background under them
// instead, and they composite to nearly the same colors.
var (
	// colScrim darkens the material, so that the desktop behind the window
	// tints it rather than showing through it.
	colScrim    = ui.RGBA(0x0e, 0x10, 0x15, 0.65)
	colBar      = ui.RGBA(0x0e, 0x10, 0x15, 0.75) // the title bar and the footer
	colCard     = ui.RGBA(0x16, 0x18, 0x20, 0.86) // the panels in the content
	colInset    = ui.RGBA(0x1a, 0x1c, 0x25, 0.92) // the fields inside a panel
	colHover    = ui.RGBA(0x22, 0x25, 0x32, 0.95) // a field the pointer is on
	colHairline = ui.RGBA(0xff, 0xff, 0xff, 0.07)
)

// appTheme is the theme the window draws its interface with.
func appTheme() *ui.Theme {
	t := ui.DarkTheme()
	t.Background = colBackground
	t.Surface = colSurface
	t.SurfaceHover = colSurfaceHover
	t.SurfacePressed = ui.Hex("#22242e")
	t.Border = colBorder
	t.Text = colText
	t.TextMuted = colTextMuted
	t.Accent = colAccent
	t.AccentHover = colAccentHover
	t.AccentPressed = colAccentPressed
	t.AccentText = colAccentText
	t.Danger = colDanger
	t.Warning = colWarning
	t.Success = colSuccess
	t.Focus = ui.RGBA(0x85, 0x6e, 0xff, 0.55)
	t.Selection = ui.RGBA(0x85, 0x6e, 0xff, 0.35)
	t.Scrollbar = ui.RGBA(0xff, 0xff, 0xff, 0.18)
	t.Radius = 10
	return t
}
