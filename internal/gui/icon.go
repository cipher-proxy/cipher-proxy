package gui

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed appicon.png
var appIconBytes []byte

// AppIcon returns the embedded application icon as a Fyne resource.
func AppIcon() fyne.Resource {
	return fyne.NewStaticResource("appicon.png", appIconBytes)
}
