package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"github.com/Maheshivara/gofroggy/pkg/gfx"
	"github.com/Maheshivara/gofroggy/pkg/gfx/text"
	"github.com/Maheshivara/gofroggy/pkg/hardware/display"
	"github.com/Maheshivara/gofroggy/pkg/hardware/input"
	"github.com/Maheshivara/gofroggy/pkg/hardware/input/mapping"
)

const (
	ColorBlack uint16 = 0x0000
	ColorRed   uint16 = 0xF800
	ColorGreen uint16 = 0x07E0
	ColorGray  uint16 = 0x7BEF
	ColorWhite uint16 = 0xFFFF
)

var displayConfig = display.Config{
	Backend:      display.BackendAuto,
	Downscale:    1,
	VSync:        false,
	UpdateScreen: false,
}

var trackedButtons = []mapping.InputButton{
	mapping.UP, mapping.DOWN, mapping.LEFT, mapping.RIGHT,
	mapping.A, mapping.B, mapping.X, mapping.Y,
	mapping.L1, mapping.R1, mapping.L2, mapping.R2,
	mapping.SELECT, mapping.START,
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

type Application struct {
	Display display.DisplayController
	Input   *input.InputController
	Drawer  *gfx.Drawer
	Font    *text.Font
	Running bool

	dirty     bool
	prevState uint32
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

	app.Running = true
	app.dirty = true

	app.Font, _ = text.LoadFont("", 20)
	_, sy := disp.Scale()
	app.Font.SetScale(sy)

	return nil
}

func (app *Application) Update() {
	app.Input.Update()

	if app.Input.IsPressed(mapping.SELECT) && app.Input.IsPressed(mapping.START) {
		logMsg("Update: SELECT + START pressed. Exiting...")
		app.Running = false
	}

	if app.Input.IsPressed(mapping.L1) && app.Input.IsPressed(mapping.R1) {
		logMsg("Update: L1 + R1 pressed. Triggering async screenshot...")
		app.Display.SaveScreenshotAsync("/mnt/sdcard/screenshots")
		logMsg("Screenshot queued successfully to /mnt/sdcard/screenshots")
	}
	var state uint32
	for i, b := range trackedButtons {
		if app.Input.IsPressed(b) {
			state |= 1 << uint(i)
		}
	}
	if state != app.prevState {
		app.prevState = state
		app.dirty = true
	}
}

func (app *Application) Draw() {
	app.dirty = false
	app.Display.Clear(ColorBlack)

	screenWidth, screenHeight := app.Display.LogicalSize()

	ascent, descent := app.Font.GetMetrics()
	fontHeight := ascent + descent

	sideMargin := 40
	topBottomMargin := 10

	titleText := "INPUT TEST"
	titleW, _ := app.Font.MeasureText(titleText)
	titleX := (screenWidth - titleW) / 2
	if titleX < sideMargin {
		titleX = sideMargin
	}
	titleY := topBottomMargin + ascent
	app.Drawer.Text.DrawText(app.Font, titleText, titleX, titleY, ColorWhite)

	exitMsg := "PRESS SELECT + START TO EXIT"
	exitW, _ := app.Font.MeasureText(exitMsg)
	exitX := (screenWidth - exitW) / 2
	if exitX < sideMargin {
		exitX = sideMargin
	}
	exitY := screenHeight - topBottomMargin - descent
	app.Drawer.Text.DrawText(app.Font, exitMsg, exitX, exitY, ColorRed)

	drawButtonStatus := func(name string, btn mapping.InputButton, x, y int) {
		color := ColorGray
		if app.Input.IsPressed(btn) {
			color = ColorGreen
		}
		app.Drawer.Text.DrawText(app.Font, name, x, y, color)
	}

	col1W, _ := app.Font.MeasureText("D-PAD RIGHT")
	col2W, _ := app.Font.MeasureText("BTN X")
	col3W, _ := app.Font.MeasureText("L2")

	availableWidth := screenWidth - (2 * sideMargin)
	totalContentWidth := col1W + col2W + col3W

	colGap := (availableWidth - totalContentWidth) / 2
	if colGap < 10 {
		colGap = 10
	}

	col1X := sideMargin
	col2X := col1X + col1W + colGap
	col3X := col2X + col2W + colGap

	startY := titleY + fontHeight + 10
	rowGap := 10

	rowY := func(rowIndex int) int {
		return startY + rowIndex*(fontHeight+rowGap)
	}

	drawButtonStatus("D-PAD UP", mapping.UP, col1X, rowY(0))
	drawButtonStatus("D-PAD DOWN", mapping.DOWN, col1X, rowY(1))
	drawButtonStatus("D-PAD LEFT", mapping.LEFT, col1X, rowY(2))
	drawButtonStatus("D-PAD RIGHT", mapping.RIGHT, col1X, rowY(3))

	drawButtonStatus("BTN A", mapping.A, col2X, rowY(0))
	drawButtonStatus("BTN B", mapping.B, col2X, rowY(1))
	drawButtonStatus("BTN X", mapping.X, col2X, rowY(2))
	drawButtonStatus("BTN Y", mapping.Y, col2X, rowY(3))

	drawButtonStatus("L1", mapping.L1, col3X, rowY(0))
	drawButtonStatus("R1", mapping.R1, col3X, rowY(1))
	drawButtonStatus("L2", mapping.L2, col3X, rowY(2))
	drawButtonStatus("R2", mapping.R2, col3X, rowY(3))

	systemY := rowY(5)
	selectW, _ := app.Font.MeasureText("SELECT")

	selectX := (screenWidth / 2) - selectW - 10
	startX := (screenWidth / 2) + 10

	drawButtonStatus("SELECT", mapping.SELECT, selectX, systemY)
	drawButtonStatus("START", mapping.START, startX, systemY)

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

	logMsg("=== Application starting ===")

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
