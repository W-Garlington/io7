package bits

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func TopBar() fyne.CanvasObject {
	return container.NewHBox(
		widget.NewButton("File", func() {}),
		widget.NewButton("Edit", func() {}),
		widget.NewButton("View", func() {}),
	)
}
