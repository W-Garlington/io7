package frontend

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"github.com/W-Garlington/io7/frontend/bits"
	"github.com/W-Garlington/io7/frontend/views"
)

func BuildLayout() fyne.CanvasObject {
	return container.NewBorder(
		bits.TopBar(),
		nil,
		bits.SideBar(),
		nil,
		views.Editor(),
	)
}
