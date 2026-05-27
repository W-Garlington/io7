package views

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

func Editor() fyne.CanvasObject {
	e := widget.NewMultiLineEntry()
	e.SetPlaceHolder("Start typing...")
	return e
}
