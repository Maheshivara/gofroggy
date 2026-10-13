package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"github.com/Maheshivara/gofroggy/pkg/gfx"
	"github.com/Maheshivara/gofroggy/pkg/gfx/primitives"
	"github.com/Maheshivara/gofroggy/pkg/gfx/text"
	"github.com/Maheshivara/gofroggy/pkg/hardware/display"
	"github.com/Maheshivara/gofroggy/pkg/hardware/input"
	"github.com/Maheshivara/gofroggy/pkg/hardware/input/mapping"
)

type ColorPreset struct {
	Name  string
	Value uint16
}

var ColorPresets = []ColorPreset{
	{"Red", 0xF800},
	{"Green", 0x07E0},
	{"Blue", 0x001F},
	{"Yellow", 0xFFE0},
	{"Cyan", 0x07FF},
	{"Magenta", 0xF81F},
	{"White", 0xFFFF},
	{"Gray", 0x7BEF},
	{"Black", 0x0000},
}

var ShapeTypes = []string{"Rectangle", "Circle", "Triangle", "Star", "Line"}

var BorderWidths = []int{1, 2, 3, 4, 6, 8, 10}

var displayConfig = display.Config{
	Backend:      display.BackendAuto,
	Downscale:    1.9,
	VSync:        false,
	UpdateScreen: false,
}

var logFile *os.File

func logMsg(msg string) {
	if logFile != nil {
		timestamp := time.Now().Format("15:04:05")
		logStr := fmt.Sprintf("[%s] %s\n", timestamp, msg)
		_, err := logFile.WriteString(logStr)
		if err != nil {
			panic(err)
		}
		err = logFile.Sync()
		if err != nil {
			panic(err)
		}
	}
	fmt.Println(msg)
}

type MenuOption int

const (
	OptionShape MenuOption = iota
	OptionIsFilled
	OptionFillColor
	OptionBorderColor
	OptionBorderWidth
	OptionCount
)

type Application struct {
	Display display.DisplayController
	Input   *input.InputController
	Drawer  *gfx.Drawer
	Font    *text.Font
	Running bool

	SelectedOption MenuOption

	ShapeIndex       int
	IsFilled         bool
	FillColorIndex   int
	BorderColorIndex int
	BorderWidthIndex int

	prevUp    bool
	prevDown  bool
	prevLeft  bool
	prevRight bool
	prevA     bool
	prevL1R1  bool

	dirty bool
}

func (app *Application) Init() error {
	logMsg("Init: Starting DisplayController initialization...")

	disp, err := display.NewDisplayController(displayConfig)
	if err != nil {
		logMsg(fmt.Sprintf("Init ERROR: NewDisplayController failed: %v", err))
		return err
	}
	app.Display = disp

	st := disp.Stats()
	lw, lh := disp.LogicalSize()
	logMsg(fmt.Sprintf("Display: backend=%s render=%dx%d logical=%dx%d downscale=%.2f",
		st.Backend, disp.Width(), disp.Height(), lw, lh, st.Downscale))

	logMsg("Init: Starting InputController initialization...")
	inputCtrl, err := input.NewInputController(nil)
	if err != nil {
		logMsg(fmt.Sprintf("Init ERROR: NewInputController failed: %v", err))
		return err
	}
	app.Input = inputCtrl

	app.Drawer = gfx.NewDrawer(app.Display)

	app.ShapeIndex = 0
	app.IsFilled = true
	app.FillColorIndex = 2
	app.BorderColorIndex = 0
	app.BorderWidthIndex = 1

	app.Font, _ = text.LoadFont("", 20)
	_, sy := disp.Scale()
	app.Font.SetScale(sy)

	app.Running = true
	app.dirty = true

	return nil
}

func (app *Application) Update() {
	app.Input.Update()

	if app.Input.IsPressed(mapping.SELECT) && app.Input.IsPressed(mapping.START) {
		logMsg("Update: SELECT + START pressed. Exiting...")
		app.Running = false
		return
	}

	currL1R1 := app.Input.IsPressed(mapping.L1) && app.Input.IsPressed(mapping.R1)
	if currL1R1 && !app.prevL1R1 {
		logMsg("Update: L1 + R1 pressed. Triggering async screenshot...")
		app.Display.SaveScreenshotAsync("/mnt/sdcard/screenshots")
	}
	app.prevL1R1 = currL1R1

	currUp := app.Input.IsPressed(mapping.UP)
	currDown := app.Input.IsPressed(mapping.DOWN)
	currLeft := app.Input.IsPressed(mapping.LEFT)
	currRight := app.Input.IsPressed(mapping.RIGHT)
	currA := app.Input.IsPressed(mapping.A)

	if currUp && !app.prevUp {
		app.SelectedOption--
		if app.SelectedOption < 0 {
			app.SelectedOption = OptionCount - 1
		}
		app.dirty = true
	}
	if currDown && !app.prevDown {
		app.SelectedOption++
		if app.SelectedOption >= OptionCount {
			app.SelectedOption = 0
		}
		app.dirty = true
	}

	if (currRight && !app.prevRight) || (currA && !app.prevA) {
		app.cycleValue(1)
		app.dirty = true
	}
	if currLeft && !app.prevLeft {
		app.cycleValue(-1)
		app.dirty = true
	}

	app.prevUp = currUp
	app.prevDown = currDown
	app.prevLeft = currLeft
	app.prevRight = currRight
	app.prevA = currA
}

func (app *Application) cycleValue(dir int) {
	switch app.SelectedOption {
	case OptionShape:
		app.ShapeIndex = (app.ShapeIndex + dir + len(ShapeTypes)) % len(ShapeTypes)

	case OptionIsFilled:
		app.IsFilled = !app.IsFilled

	case OptionFillColor:
		app.FillColorIndex = (app.FillColorIndex + dir + len(ColorPresets)) % len(ColorPresets)

	case OptionBorderColor:
		app.BorderColorIndex = (app.BorderColorIndex + dir + len(ColorPresets)) % len(ColorPresets)

	case OptionBorderWidth:
		app.BorderWidthIndex = (app.BorderWidthIndex + dir + len(BorderWidths)) % len(BorderWidths)
	}
}

func (app *Application) Draw() {
	app.dirty = false
	app.Display.Clear(ColorPresets[8].Value)

	screenWidth, screenHeight := app.Display.LogicalSize()

	ascent, descent := app.Font.GetMetrics()
	fontHeight := ascent + descent
	if fontHeight == 0 {
		fontHeight = 18
	}

	titleText := "PRIMITIVES TEST"
	titleW, _ := app.Font.MeasureText(titleText)
	app.Drawer.Text.DrawText(app.Font, titleText, (screenWidth-titleW)/2, 10+ascent, ColorPresets[6].Value)

	canvasCenter := primitives.Point{X: screenWidth / 2, Y: (screenHeight / 2) - 25}

	drawCfg := primitives.DrawConfig{
		Color:       ColorPresets[app.BorderColorIndex].Value,
		FillColor:   ColorPresets[app.FillColorIndex].Value,
		IsFilled:    app.IsFilled,
		BorderWidth: BorderWidths[app.BorderWidthIndex],
	}

	switch ShapeTypes[app.ShapeIndex] {
	case "Rectangle":
		rectW, rectH := 120, 80
		app.Drawer.Primitives.DrawRectangle(
			primitives.Rectangle{
				Origin: primitives.Point{X: canvasCenter.X - (rectW / 2), Y: canvasCenter.Y - (rectH / 2)},
				Width:  rectW,
				Height: rectH,
			},
			drawCfg,
		)

	case "Circle":
		app.Drawer.Primitives.DrawCircle(
			primitives.Circle{
				Center: canvasCenter,
				Radius: 45,
			},
			drawCfg,
		)

	case "Triangle":
		app.Drawer.Primitives.DrawPolygon(
			primitives.Polygon{
				Points: []primitives.Point{
					{X: canvasCenter.X, Y: canvasCenter.Y - 55},
					{X: canvasCenter.X + 60, Y: canvasCenter.Y + 40},
					{X: canvasCenter.X - 60, Y: canvasCenter.Y + 40},
				},
			},
			drawCfg,
		)

	case "Star":
		app.Drawer.Primitives.DrawPolygon(
			primitives.Polygon{
				Points: []primitives.Point{
					{X: canvasCenter.X, Y: canvasCenter.Y - 50},
					{X: canvasCenter.X + 15, Y: canvasCenter.Y - 15},
					{X: canvasCenter.X + 50, Y: canvasCenter.Y - 15},
					{X: canvasCenter.X + 22, Y: canvasCenter.Y + 10},
					{X: canvasCenter.X + 35, Y: canvasCenter.Y + 45},
					{X: canvasCenter.X, Y: canvasCenter.Y + 25},
					{X: canvasCenter.X - 35, Y: canvasCenter.Y + 45},
					{X: canvasCenter.X - 22, Y: canvasCenter.Y + 10},
					{X: canvasCenter.X - 50, Y: canvasCenter.Y - 15},
					{X: canvasCenter.X - 15, Y: canvasCenter.Y - 15},
				},
			},
			drawCfg,
		)

	case "Line":
		app.Drawer.Primitives.DrawLine(
			primitives.Point{X: canvasCenter.X - 70, Y: canvasCenter.Y - 35},
			primitives.Point{X: canvasCenter.X + 70, Y: canvasCenter.Y + 35},
			drawCfg.Color,
			drawCfg.BorderWidth,
		)
	}

	menuYStart := screenHeight - (int(OptionCount) * (fontHeight + 3)) - 20

	options := []struct {
		label string
		value string
	}{
		{"Shape", ShapeTypes[app.ShapeIndex]},
		{"Filled", fmt.Sprintf("%t", app.IsFilled)},
		{"Fill Color", ColorPresets[app.FillColorIndex].Name},
		{"Border Color", ColorPresets[app.BorderColorIndex].Name},
		{"Border Width", fmt.Sprintf("%d px", BorderWidths[app.BorderWidthIndex])},
	}

	for i, opt := range options {
		y := menuYStart + (i * (fontHeight + 3))

		isCurrent := MenuOption(i) == app.SelectedOption

		labelColor := ColorPresets[7].Value
		valueColor := ColorPresets[6].Value

		if isCurrent {
			labelColor = ColorPresets[3].Value
			valueColor = ColorPresets[4].Value
		}

		app.Drawer.Text.DrawText(app.Font, opt.label, 20, y+ascent, labelColor)

		valText := opt.value
		if isCurrent {
			valText = fmt.Sprintf("< %s >", opt.value)
		}

		valW, _ := app.Font.MeasureText(valText)
		app.Drawer.Text.DrawText(app.Font, valText, screenWidth-valW-20, y+ascent, valueColor)
	}

	footerText := "PRESS SELECT + START TO EXIT"
	footW, _ := app.Font.MeasureText(footerText)
	app.Drawer.Text.DrawText(app.Font, footerText, (screenWidth-footW)/2, screenHeight-5, ColorPresets[7].Value)

	err := app.Display.Update()
	if err != nil {
		logMsg("ERROR: fail to update display")
		app.Running = false
	}
}

func (app *Application) Run() {
	logMsg("Run: Entering main loop...")
	ticker := time.NewTicker(time.Second / 60)
	defer ticker.Stop()

	for app.Running {
		app.Update()
		if app.dirty {
			app.Draw()
		}
		<-ticker.C
	}
}

func main() {
	var err error
	logFile, err = os.OpenFile("/mnt/sdcard/test.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		fmt.Printf("Failed to open log file: %v\n", err)
	} else {
		defer func() {
			err := logFile.Close()
			if err != nil {
				panic(err)
			}
		}()
	}

	logMsg("=== Primitives Tester Application starting ===")

	defer func() {
		if r := recover(); r != nil {
			logMsg(fmt.Sprintf("CRASH PANIC DETECTED: %v", r))
			logMsg(string(debug.Stack()))
		}
	}()

	app := &Application{}
	if err := app.Init(); err != nil {
		logMsg(fmt.Sprintf("Main ERROR: Init failed: %v", err))
		return
	}
	defer app.Display.Close()
	defer func() {
		err := app.Input.Close()
		if err != nil {
			panic(err)
		}
	}()

	app.Run()
	logMsg("=== Application finished gracefully ===")
}
