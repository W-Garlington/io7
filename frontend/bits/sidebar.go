package bits

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func SideBar() fyne.CanvasObject {
	return container.NewVBox(
		widget.NewButton("Action 1", func() {}),
		widget.NewButton("Action 2", func() {}),
		widget.NewButton("Action 3", func() {}),
	)
}
