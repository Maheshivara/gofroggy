package main

import (
	"embed"
	"fmt"
	"os"
	"time"

	"github.com/Maheshivara/gofroggy/pkg/gfx"
	"github.com/Maheshivara/gofroggy/pkg/hardware/display"
	"github.com/Maheshivara/gofroggy/pkg/hardware/input"
	"github.com/Maheshivara/gofroggy/pkg/hardware/input/mapping"
	"github.com/Maheshivara/gofroggy/pkg/scene"
)

//
//go:embed img/*.png
var imgFS embed.FS

const (
	screenshotDir = "/mnt/sdcard/screenshots"
	logFilePath   = "/mnt/sdcard/test.log"
)

var logFile *os.File

var sceneButtons = []mapping.InputButton{
	mapping.UP, mapping.DOWN, mapping.LEFT, mapping.RIGHT, mapping.A,
}

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

func run() error {
	disp, err := display.NewDisplayController(display.Config{Downscale: 1})
	if err != nil {
		return err
	}
	defer disp.Close()

	in, err := input.NewInputController(nil)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	drawer := gfx.NewDrawer(disp)

	mgr := scene.NewSceneManager()
	w, h := disp.LogicalSize()
	if err := mgr.Push(NewSpriteDemo(imgFS, w, h)); err != nil {
		return err
	}

	prevButtons := make([]bool, len(sceneButtons))
	prevShot := false

	ticker := time.NewTicker(time.Second / 60)
	defer ticker.Stop()

	last := time.Now()
	for {
		now := time.Now()
		dt := now.Sub(last).Seconds()
		last = now

		in.Update()

		if in.IsPressed(mapping.SELECT) && in.IsPressed(mapping.START) {
			return nil
		}
		shot := in.IsPressed(mapping.L1) && in.IsPressed(mapping.R1)
		if shot && !prevShot {
			logMsg("Update: L1 + R1 pressed. Triggering screenshot...")
			disp.SaveScreenshotAsync(screenshotDir)
		}
		prevShot = shot

		for i, b := range sceneButtons {
			if down := in.IsPressed(b); down != prevButtons[i] {
				mgr.HandleInput(b, down)
				prevButtons[i] = down
			}
		}

		if err := mgr.Update(dt, in); err != nil {
			return err
		}

		disp.Clear(0)
		mgr.Draw(drawer)
		if err := disp.Update(); err != nil {
			return err
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

	err = run()
	if err != nil {
		logMsg(fmt.Sprintf("running err %v", err))
		return
	}

	logMsg("=== Application finished gracefully ===")
}
