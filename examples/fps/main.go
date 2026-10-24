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

const (
	ColorBlack  uint16 = 0x0000
	ColorWhite  uint16 = 0xFFFF
	ColorGreen  uint16 = 0x07E0
	ColorCyan   uint16 = 0x07FF
	ColorYellow uint16 = 0xFFE0
	ColorRed    uint16 = 0xF800
)

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

	BarX     float64
	BarSpeed float64

	FPS        float64
	frameCount int
	fpsTimer   float64

	lastDraw time.Duration
}

func (app *Application) Init(cfg display.Config) error {
	logMsg("Init: Starting DisplayController initialization...")
	disp, err := display.NewDisplayController(cfg)
	if err != nil {
		return fmt.Errorf("NewDisplayController failed: %w", err)
	}
	app.Display = disp
	st := disp.Stats()
	lw, lh := disp.LogicalSize()
	logMsg(fmt.Sprintf("Display: backend=%s render=%dx%d logical=%dx%d downscale=%.2f vsync requested=%v active=%v",
		st.Backend, disp.Width(), disp.Height(), lw, lh, st.Downscale, cfg.VSync, st.VSyncActive))

	logMsg("Init: Starting InputController initialization...")
	inputCtrl, err := input.NewInputController(nil)
	if err != nil {
		return fmt.Errorf("NewInputController failed: %w", err)
	}
	app.Input = inputCtrl
	app.Font, _ = text.LoadFont("", 20)
	_, sy := disp.Scale()
	app.Font.SetScale(sy)

	app.Drawer = gfx.NewDrawer(app.Display)

	app.BarSpeed = 240.0

	app.Running = true
	return nil
}

func (app *Application) Update(dt float64) {
	app.Input.Update()

	if app.Input.IsPressed(mapping.SELECT) && app.Input.IsPressed(mapping.START) {
		logMsg("Update: SELECT + START pressed. Exiting...")
		app.Running = false
		return
	}

	if app.Input.IsPressed(mapping.L1) && app.Input.IsPressed(mapping.R1) {
		logMsg("Update: L1 + R1 pressed. Triggering async screenshot...")
		app.Display.SaveScreenshotAsync("/mnt/sdcard/screenshots")
		logMsg("Screenshot queued successfully to /mnt/sdcard/screenshots")
	}

	app.frameCount++
	app.fpsTimer += dt
	if app.fpsTimer >= 1.0 {
		app.FPS = float64(app.frameCount) / app.fpsTimer
		app.frameCount = 0
		app.fpsTimer = 0.0
	}

	logicalW, _ := app.Display.LogicalSize()

	app.BarX += app.BarSpeed * dt
	if app.BarX >= float64(logicalW-20) {
		app.BarX = float64(logicalW - 20)
		app.BarSpeed = -app.BarSpeed
	} else if app.BarX <= 0 {
		app.BarX = 0
		app.BarSpeed = -app.BarSpeed
	}
}

func (app *Application) Draw() {
	if app.frameCount%60 == 0 {
		st := app.Display.Stats()
		logMsg(fmt.Sprintf("draw=%v copy=%v wait=%v show=%v fps=%.1f",
			app.lastDraw, st.Copy, st.Wait, st.Show, app.FPS))
	}
	drawStart := time.Now()
	app.Display.Clear(ColorBlack)

	screenWidth, screenHeight := app.Display.LogicalSize()

	barXInt := int(app.BarX)
	app.Drawer.Primitives.DrawRectangle(
		primitives.Rectangle{
			Origin: primitives.Point{X: barXInt, Y: 40},
			Width:  20,
			Height: screenHeight - 80,
		},
		primitives.DrawConfig{
			Color:     ColorCyan,
			FillColor: ColorCyan,
			IsFilled:  true,
		},
	)

	ascent, _ := app.Font.GetMetrics()

	fpsStr := fmt.Sprintf("FPS: %.1f", app.FPS)
	fpsColor := ColorGreen
	if app.FPS < 30 {
		fpsColor = ColorRed
	}
	app.Drawer.Text.DrawText(app.Font, fpsStr, 10, 10+ascent, fpsColor)

	hudText := "SEL+START: EXIT"
	hudW, _ := app.Font.MeasureText(hudText)
	app.Drawer.Text.DrawText(app.Font, hudText, (screenWidth-hudW)/2, screenHeight-10, ColorWhite)

	app.lastDraw = time.Since(drawStart)

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

	lastTime := time.Now()

	for app.Running {
		now := time.Now()
		dt := now.Sub(lastTime).Seconds()
		lastTime = now

		app.Update(dt)
		app.Draw()

		<-ticker.C
	}
}

func main() {

	cfg := display.Config{Downscale: 1, VSync: false, UpdateScreen: false, Backend: display.BackendAuto}

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

	logMsg("=== FPS Test starting ===")

	defer func() {
		if r := recover(); r != nil {
			logMsg(fmt.Sprintf("CRASH PANIC DETECTED: %v", r))
			logMsg(string(debug.Stack()))
		}
	}()

	app := &Application{}
	if err := app.Init(cfg); err != nil {
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
