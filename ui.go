package main

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

const maxLines = 10000

// AppUI holds all UI state and widgets.
type AppUI struct {
	window fyne.Window
	serial *SerialManager

	// Widgets
	portSelect    *widget.Select
	baudSelect    *widget.Select
	connectBtn    *widget.Button
	clearBtn      *widget.Button
	exportBtn     *widget.Button
	autoscrollChk *widget.Check
	timestampChk  *widget.Check
	output        *widget.List
	refreshBtn    *widget.Button

	// State
	mu             sync.Mutex
	lines          []SerialLine
	displayLines   []string
	autoscroll     bool
	showTimestamp  bool
	connected      atomic.Bool
	savedTemplates []string // user-saved CSV header templates
}

var standardBaudRates = []string{
	"300", "1200", "2400", "4800", "9600", "19200",
	"38400", "57600", "74880", "115200", "230400",
	"250000", "500000", "1000000", "2000000",
}

func NewAppUI(window fyne.Window, serial *SerialManager) *AppUI {
	templates, _ := LoadTemplates()
	ui := &AppUI{
		window:         window,
		serial:         serial,
		autoscroll:     true,
		savedTemplates: templates,
	}
	ui.build()
	return ui
}

func (ui *AppUI) build() {
	// Port selection
	ui.portSelect = widget.NewSelect([]string{}, nil)
	ui.portSelect.PlaceHolder = "Select COM Port"

	ui.refreshBtn = widget.NewButton("Refresh", func() {
		ui.refreshPorts()
	})
	ui.refreshPorts()

	// Baud rate selection
	ui.baudSelect = widget.NewSelect(standardBaudRates, nil)
	ui.baudSelect.SetSelected("9600")

	// Connect/Disconnect button
	ui.connectBtn = widget.NewButton("Connect", func() {
		ui.toggleConnection()
	})

	// Clear button
	ui.clearBtn = widget.NewButton("Clear", func() {
		ui.mu.Lock()
		ui.lines = nil
		ui.displayLines = nil
		ui.mu.Unlock()
		ui.output.Refresh()
	})

	// Export button
	ui.exportBtn = widget.NewButton("Export CSV", func() {
		ui.showExportDialog()
	})

	// Autoscroll checkbox
	ui.autoscrollChk = widget.NewCheck("Autoscroll", func(checked bool) {
		ui.mu.Lock()
		ui.autoscroll = checked
		ui.mu.Unlock()
	})
	ui.autoscrollChk.SetChecked(true)

	// Timestamp checkbox
	ui.timestampChk = widget.NewCheck("Timestamps", func(checked bool) {
		ui.mu.Lock()
		ui.showTimestamp = checked
		ui.rebuildDisplayLines()
		ui.mu.Unlock()
		ui.output.Refresh()
	})

	// Output list — copy the display text outside the lock to avoid deadlock
	// with Fyne's internal re-entrant calls.
	ui.output = widget.NewList(
		func() int {
			ui.mu.Lock()
			defer ui.mu.Unlock()
			return len(ui.displayLines)
		},
		func() fyne.CanvasObject {
			label := widget.NewLabel("")
			label.TextStyle = fyne.TextStyle{Monospace: true}
			return label
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			ui.mu.Lock()
			var text string
			if id < len(ui.displayLines) {
				text = ui.displayLines[id]
			}
			ui.mu.Unlock()
			obj.(*widget.Label).SetText(text)
		},
	)

	// Layout
	portRow := container.NewHBox(
		widget.NewLabel("Port:"),
		ui.portSelect,
		ui.refreshBtn,
		widget.NewLabel("Baud:"),
		ui.baudSelect,
		ui.connectBtn,
	)

	optionsRow := container.NewHBox(
		ui.autoscrollChk,
		ui.timestampChk,
		layout.NewSpacer(),
		ui.clearBtn,
		ui.exportBtn,
	)

	toolbar := container.NewVBox(portRow, optionsRow)
	content := container.NewBorder(toolbar, nil, nil, nil, ui.output)
	ui.window.SetContent(content)
}

func (ui *AppUI) refreshPorts() {
	ports := ui.serial.AvailablePorts()
	ui.portSelect.Options = ports
	if len(ports) > 0 {
		ui.portSelect.SetSelected(ports[0])
	}
	ui.portSelect.Refresh()
}

func (ui *AppUI) setDisconnectedState() {
	ui.connected.Store(false)
	ui.connectBtn.SetText("Connect")
	ui.portSelect.Enable()
	ui.baudSelect.Enable()
}

func (ui *AppUI) toggleConnection() {
	if ui.connected.Load() {
		ui.serial.Disconnect()
		ui.setDisconnectedState()
		return
	}

	portName := ui.portSelect.Selected
	if portName == "" {
		dialog.ShowError(fmt.Errorf("no COM port selected"), ui.window)
		return
	}

	baudRate, err := strconv.Atoi(ui.baudSelect.Selected)
	if err != nil {
		dialog.ShowError(fmt.Errorf("invalid baud rate: %s", ui.baudSelect.Selected), ui.window)
		return
	}

	if err := ui.serial.Connect(portName, baudRate); err != nil {
		dialog.ShowError(fmt.Errorf("failed to connect: %w", err), ui.window)
		return
	}

	ui.connected.Store(true)
	ui.connectBtn.SetText("Disconnect")
	ui.portSelect.Disable()
	ui.baudSelect.Disable()

	ch, errCh := ui.serial.StartReading()
	go ui.consumeSerial(ch, errCh)
}

func (ui *AppUI) consumeSerial(ch <-chan SerialLine, errCh <-chan error) {
	for line := range ch {
		ui.mu.Lock()
		ui.lines = append(ui.lines, line)

		// Bound memory
		if len(ui.lines) > maxLines {
			ui.lines = ui.lines[len(ui.lines)-maxLines:]
		}

		ui.displayLines = append(ui.displayLines, ui.formatLine(line))
		if len(ui.displayLines) > maxLines {
			ui.displayLines = ui.displayLines[len(ui.displayLines)-maxLines:]
		}

		shouldScroll := ui.autoscroll
		count := len(ui.displayLines)
		ui.mu.Unlock()

		fyne.Do(func() {
			ui.output.Refresh()
			if shouldScroll && count > 0 {
				ui.output.ScrollToBottom()
			}
		})
	}

	// Channel closed — check if there was an error
	select {
	case err := <-errCh:
		if err != nil {
			fyne.Do(func() {
				ui.setDisconnectedState()
				dialog.ShowError(fmt.Errorf("serial port error: %w", err), ui.window)
			})
		}
	default:
		// Clean shutdown (user disconnected)
	}
}

func (ui *AppUI) formatLine(line SerialLine) string {
	if ui.showTimestamp {
		return fmt.Sprintf("[%s] %s", line.Timestamp.Format("15:04:05.000"), line.Data)
	}
	return line.Data
}

// rebuildDisplayLines regenerates all display strings (called when timestamp toggle changes).
// Must be called with ui.mu held.
func (ui *AppUI) rebuildDisplayLines() {
	ui.displayLines = make([]string, len(ui.lines))
	for i, line := range ui.lines {
		ui.displayLines[i] = ui.formatLine(line)
	}
}

func (ui *AppUI) showExportDialog() {
	ui.mu.Lock()
	lineCount := len(ui.lines)
	ui.mu.Unlock()

	if lineCount == 0 {
		dialog.ShowInformation("Export", "No data to export.", ui.window)
		return
	}

	// Options
	includeTimestamps := widget.NewCheck("Include timestamps", nil)

	filterByTime := widget.NewCheck("Filter by time range", nil)

	startEntry := widget.NewEntry()
	startEntry.SetPlaceHolder("Start (HH:MM:SS)")
	startEntry.Disable()

	endEntry := widget.NewEntry()
	endEntry.SetPlaceHolder("End (HH:MM:SS)")
	endEntry.Disable()

	filterByTime.OnChanged = func(checked bool) {
		if checked {
			startEntry.Enable()
			endEntry.Enable()
		} else {
			startEntry.Disable()
			endEntry.Disable()
		}
	}

	// Header source selection: None, Template, Paste, File
	headerSourceSelect := widget.NewSelect([]string{"None", "Template", "Paste", "File"}, nil)
	headerSourceSelect.SetSelected("None")

	// User-saved templates
	headerTemplateSelect := widget.NewSelect(ui.savedTemplates, nil)
	headerTemplateSelect.PlaceHolder = "Select saved template..."
	headerTemplateSelect.Disable()

	saveTemplateBtn := widget.NewButton("Save Current as Template", nil)
	saveTemplateBtn.Disable()
	deleteTemplateBtn := widget.NewButton("Delete Selected", nil)
	deleteTemplateBtn.Disable()

	// Paste entry
	headerPasteEntry := widget.NewEntry()
	headerPasteEntry.SetPlaceHolder("e.g. Time,Temp,Humidity")
	headerPasteEntry.Disable()

	// Save template from paste entry
	saveTemplateBtn.OnTapped = func() {
		text := strings.TrimSpace(headerPasteEntry.Text)
		if text == "" {
			dialog.ShowInformation("Template", "Enter a header in the Paste field first.", ui.window)
			return
		}
		// Avoid duplicates
		for _, t := range ui.savedTemplates {
			if t == text {
				dialog.ShowInformation("Template", "This template already exists.", ui.window)
				return
			}
		}
		ui.savedTemplates = append(ui.savedTemplates, text)
		SaveTemplates(ui.savedTemplates)
		headerTemplateSelect.Options = ui.savedTemplates
		headerTemplateSelect.Refresh()
		dialog.ShowInformation("Template", "Template saved.", ui.window)
	}

	// Delete selected template
	deleteTemplateBtn.OnTapped = func() {
		sel := headerTemplateSelect.Selected
		if sel == "" {
			return
		}
		for i, t := range ui.savedTemplates {
			if t == sel {
				ui.savedTemplates = append(ui.savedTemplates[:i], ui.savedTemplates[i+1:]...)
				break
			}
		}
		SaveTemplates(ui.savedTemplates)
		headerTemplateSelect.Options = ui.savedTemplates
		headerTemplateSelect.ClearSelected()
		headerTemplateSelect.Refresh()
	}

	// File browse
	headerPathLabel := widget.NewLabel("No file selected")
	headerBrowseBtn := widget.NewButton("Browse...", nil)
	headerBrowseBtn.Disable()

	var customHeaderPath string
	headerBrowseBtn.OnTapped = func() {
		fd := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil || reader == nil {
				return
			}
			customHeaderPath = reader.URI().Path()
			if len(customHeaderPath) > 2 && customHeaderPath[0] == '/' && customHeaderPath[2] == ':' {
				customHeaderPath = customHeaderPath[1:]
			}
			headerPathLabel.SetText(reader.URI().Name())
			reader.Close()
		}, ui.window)
		fd.Show()
	}

	headerSourceSelect.OnChanged = func(selected string) {
		headerTemplateSelect.Disable()
		headerPasteEntry.Disable()
		headerBrowseBtn.Disable()
		saveTemplateBtn.Disable()
		deleteTemplateBtn.Disable()
		switch selected {
		case "Template":
			headerTemplateSelect.Enable()
			deleteTemplateBtn.Enable()
		case "Paste":
			headerPasteEntry.Enable()
			saveTemplateBtn.Enable()
		case "File":
			headerBrowseBtn.Enable()
		}
	}

	form := widget.NewForm(
		widget.NewFormItem("Timestamps", includeTimestamps),
		widget.NewFormItem("Time Filter", filterByTime),
		widget.NewFormItem("Start", startEntry),
		widget.NewFormItem("End", endEntry),
		widget.NewFormItem("Header Source", headerSourceSelect),
		widget.NewFormItem("Template", container.NewHBox(headerTemplateSelect, deleteTemplateBtn)),
		widget.NewFormItem("Paste Header", container.NewVBox(headerPasteEntry, saveTemplateBtn)),
		widget.NewFormItem("Header File", container.NewHBox(headerPathLabel, headerBrowseBtn)),
	)

	dialog.ShowCustomConfirm("Export CSV Options", "Export", "Cancel", form, func(confirmed bool) {
		if !confirmed {
			return
		}

		// Build export options
		opts := CSVExportOptions{
			IncludeTimestamps: includeTimestamps.Checked,
			FilterByTime:      filterByTime.Checked,
		}

		if filterByTime.Checked {
			now := time.Now()
			startText := strings.TrimSpace(startEntry.Text)
			endText := strings.TrimSpace(endEntry.Text)

			if startText != "" {
				t, err := time.Parse("15:04:05", startText)
				if err != nil {
					dialog.ShowError(fmt.Errorf("invalid start time format (use HH:MM:SS): %s", startText), ui.window)
					return
				}
				opts.StartTime = time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.Local)
			}
			if endText != "" {
				t, err := time.Parse("15:04:05", endText)
				if err != nil {
					dialog.ShowError(fmt.Errorf("invalid end time format (use HH:MM:SS): %s", endText), ui.window)
					return
				}
				opts.EndTime = time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.Local)
			} else {
				// No end time specified — include everything up to now
				opts.EndTime = now
			}
		}

		switch headerSourceSelect.Selected {
		case "Template":
			if headerTemplateSelect.Selected != "" {
				opts.CustomHeader = strings.Split(headerTemplateSelect.Selected, ",")
			}
		case "Paste":
			text := strings.TrimSpace(headerPasteEntry.Text)
			if text != "" {
				opts.CustomHeader = strings.Split(text, ",")
				for i := range opts.CustomHeader {
					opts.CustomHeader[i] = strings.TrimSpace(opts.CustomHeader[i])
				}
			}
		case "File":
			if customHeaderPath != "" {
				header, err := ParseCustomHeader(customHeaderPath)
				if err != nil {
					dialog.ShowError(fmt.Errorf("failed to load header: %w", err), ui.window)
					return
				}
				opts.CustomHeader = header
			}
		}

		// Show save dialog
		fd := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
			if err != nil || writer == nil {
				return
			}
			writer.Close()

			savePath := writer.URI().Path()
			if len(savePath) > 2 && savePath[0] == '/' && savePath[2] == ':' {
				savePath = savePath[1:]
			}
			opts.FilePath = savePath

			ui.mu.Lock()
			linesCopy := make([]SerialLine, len(ui.lines))
			copy(linesCopy, ui.lines)
			ui.mu.Unlock()

			if err := ExportCSV(linesCopy, opts); err != nil {
				dialog.ShowError(err, ui.window)
				return
			}

			dialog.ShowInformation("Export", fmt.Sprintf("Exported %d lines to CSV.", len(linesCopy)), ui.window)
		}, ui.window)
		fd.SetFileName("serial_data.csv")
		fd.Show()
	}, ui.window)
}
