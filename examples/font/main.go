package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/Maheshivara/gofroggy/pkg/gfx"
	"github.com/Maheshivara/gofroggy/pkg/gfx/text"
	"github.com/Maheshivara/gofroggy/pkg/hardware/display"
	"github.com/Maheshivara/gofroggy/pkg/hardware/input"
	"github.com/Maheshivara/gofroggy/pkg/hardware/input/mapping"
)

const (
	FontDir = "/mnt/sdcard/frogui/fonts"

	FontSize = 20

	ColorBlack  uint16 = 0x0000
	ColorRed    uint16 = 0xF800
	ColorGreen  uint16 = 0x07E0
	ColorBlue   uint16 = 0x001F
	ColorYellow uint16 = 0xFFE0
	ColorCyan   uint16 = 0x07FF
	ColorGray   uint16 = 0x7BEF
	ColorWhite  uint16 = 0xFFFF
)

var displayConfig = display.Config{
	Backend:      display.BackendBridge,
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

type LazyFontItem struct {
	Name string
	Path string
}

type Application struct {
	Display     display.DisplayController
	Input       *input.InputController
	Drawer      *gfx.Drawer
	FontPaths   []LazyFontItem
	FontIndex   int
	FontCache   map[string]*text.Font
	CurrentFont *text.Font
	Running     bool

	prevLeft  bool
	prevRight bool
	prevL1R1  bool

	dirty bool
}

func scanFontPaths() []LazyFontItem {
	var items []LazyFontItem

	items = append(items, LazyFontItem{
		Name: "Built-in Basic (8x8)",
		Path: "",
	})

	entries, err := os.ReadDir(FontDir)
	if err != nil {
		logMsg(fmt.Sprintf("scanFontPaths WARNING: Could not read %s: %v", FontDir, err))
		return items
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext == ".ttf" || ext == ".otf" {
			fullPath := filepath.Join(FontDir, entry.Name())
			items = append(items, LazyFontItem{
				Name: entry.Name(),
				Path: fullPath,
			})
			logMsg(fmt.Sprintf("Discovered Font Path: %s", entry.Name()))
		}
	}

	return items
}

func (app *Application) loadSelectedFont() {
	item := app.FontPaths[app.FontIndex]

	if cachedFont, exists := app.FontCache[item.Path]; exists {
		app.CurrentFont = cachedFont
		return
	}

	logMsg(fmt.Sprintf("Loading Font [%d/%d]: %s", app.FontIndex+1, len(app.FontPaths), item.Name))
	loaded, _ := text.LoadFont(item.Path, FontSize)

	_, sy := app.Display.Scale()
	loaded.SetScale(sy)

	app.FontCache[item.Path] = loaded
	app.CurrentFont = loaded
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

	app.FontCache = make(map[string]*text.Font)
	app.FontPaths = scanFontPaths()
	app.FontIndex = 0

	app.loadSelectedFont()

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
		logMsg("Screenshot queued successfully to /mnt/sdcard/screenshots")
	}
	app.prevL1R1 = currL1R1

	currLeft := app.Input.IsPressed(mapping.LEFT)
	currRight := app.Input.IsPressed(mapping.RIGHT)

	if currLeft && !app.prevLeft {
		app.FontIndex--
		if app.FontIndex < 0 {
			app.FontIndex = len(app.FontPaths) - 1
		}
		app.loadSelectedFont()
		app.dirty = true
	}

	if currRight && !app.prevRight {
		app.FontIndex++
		if app.FontIndex >= len(app.FontPaths) {
			app.FontIndex = 0
		}
		app.loadSelectedFont()
		app.dirty = true
	}

	app.prevLeft = currLeft
	app.prevRight = currRight
}

func (app *Application) Draw() {
	app.dirty = false
	app.Display.Clear(ColorBlack)

	screenWidth, screenHeight := app.Display.LogicalSize()

	sideMargin := 20
	topBottomMargin := 10

	currentPathItem := app.FontPaths[app.FontIndex]

	ascent, descent := app.CurrentFont.GetMetrics()
	lineHeight := ascent + descent
	lineGap := int(float64(lineHeight) * 0.20)
	if lineGap < 2 {
		lineGap = 2
	}

	currentTopY := topBottomMargin

	titleText := "FONT TEST"
	titleW, _ := app.CurrentFont.MeasureText(titleText)
	titleX := (screenWidth - titleW) / 2
	if titleX < sideMargin {
		titleX = sideMargin
	}
	app.Drawer.Text.DrawText(app.CurrentFont, titleText, titleX, currentTopY+ascent, ColorWhite)
	currentTopY += lineHeight + lineGap

	infoText := fmt.Sprintf("[%d/%d] %s", app.FontIndex+1, len(app.FontPaths), currentPathItem.Name)
	infoW, _ := app.CurrentFont.MeasureText(infoText)
	infoX := (screenWidth - infoW) / 2
	if infoX < sideMargin {
		infoX = sideMargin
	}
	app.Drawer.Text.DrawText(app.CurrentFont, infoText, infoX, currentTopY+ascent, ColorGreen)
	currentTopY += lineHeight + lineGap + 2

	sUpper := "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	app.Drawer.Text.DrawText(app.CurrentFont, sUpper, sideMargin, currentTopY+ascent, ColorWhite)
	currentTopY += lineHeight + lineGap

	sLower := "abcdefghijklmnopqrstuvwxyz"
	app.Drawer.Text.DrawText(app.CurrentFont, sLower, sideMargin, currentTopY+ascent, ColorBlue)
	currentTopY += lineHeight + lineGap

	sNumbers := "0123456789"
	app.Drawer.Text.DrawText(app.CurrentFont, sNumbers, sideMargin, currentTopY+ascent, ColorYellow)
	currentTopY += lineHeight + lineGap

	sSymbols := "!@#$%^&*()_+-=[]{}\\|;:'\",.<>/?`~"
	app.Drawer.Text.DrawText(app.CurrentFont, sSymbols, sideMargin, currentTopY+ascent, ColorCyan)
	currentTopY += lineHeight + lineGap

	sArabic := "مرحبا بك في إطار العمل"
	app.Drawer.Text.DrawText(app.CurrentFont, sArabic, sideMargin, currentTopY+ascent, ColorYellow)
	currentTopY += lineHeight + lineGap

	sCJK := "こんにちは 世界 漢字 測試"
	app.Drawer.Text.DrawText(app.CurrentFont, sCJK, sideMargin, currentTopY+ascent, ColorCyan)

	exitMsg := "PRESS SELECT + START TO EXIT"
	exitW, _ := app.CurrentFont.MeasureText(exitMsg)
	exitX := (screenWidth - exitW) / 2
	if exitX < sideMargin {
		exitX = sideMargin
	}
	exitTopY := screenHeight - topBottomMargin - lineHeight
	app.Drawer.Text.DrawText(app.CurrentFont, exitMsg, exitX, exitTopY+ascent, ColorRed)

	navMsg := "< D-LEFT | D-RIGHT > CHANGE FONT"
	navW, _ := app.CurrentFont.MeasureText(navMsg)
	navX := (screenWidth - navW) / 2
	if navX < sideMargin {
		navX = sideMargin
	}
	navTopY := exitTopY - lineHeight - lineGap
	app.Drawer.Text.DrawText(app.CurrentFont, navMsg, navX, navTopY+ascent, ColorGray)

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

	logMsg("=== Font Tester Application starting ===")

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
