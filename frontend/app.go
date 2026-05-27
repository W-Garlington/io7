package frontend

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
)

func Run() {
	a := app.New()
	w := a.NewWindow("io7")
	w.SetContent(BuildLayout())
	w.Resize(fyne.NewSize(900, 600))
	w.ShowAndRun()
}
