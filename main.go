package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
)

func main() {
	a := app.NewWithID("com.github.craigs.serial-monitor")
	w := a.NewWindow("Serial Monitor")
	w.Resize(fyne.NewSize(800, 500))

	sm := NewSerialManager()
	NewAppUI(w, sm)

	w.ShowAndRun()
}
