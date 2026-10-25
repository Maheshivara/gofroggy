package display

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// BridgeDisplayController shows frames through the driver_bridge helper
// process. It needs no cgo; the frame buffer is shared memory.
type BridgeDisplayController struct {
	frame

	vsyncActive bool

	lastCopy, lastWait, lastShow time.Duration

	check        bool   // compare frames, skipping or shrinking unchanged ones
	skipped      uint64 // frames skipped because they were identical
	rowsOK       bool   // the bridge can copy just some rows ("R" command)
	cur          int    // staging buffer the bridge fills next (it alternates)
	pend         [2]damage
	rowsCmd      [5]byte
	outputPrimed bool
	sharedPixels []uint16

	cmd       *exec.Cmd
	stdinPipe io.WriteCloser
	reader    *bufio.Reader
	shm       []byte
	ack       [12]byte
}

var _ DisplayController = (*BridgeDisplayController)(nil)

// bridgeFrameCmd tells the bridge that a whole frame is ready.
var bridgeFrameCmd = []byte{'F'}

// A bridge that can copy only part of a frame also accepts 'R', then the
// first and last changed row as little-endian uint16: five bytes in all.

// NewBridgeDisplayController starts driver_bridge and maps its frame buffer.
func NewBridgeDisplayController(cfg Config) (*BridgeDisplayController, error) {
	panelW, panelH := readPanelSize()
	var baseFrame frame
	if err := baseFrame.configure(cfg, panelW, panelH); err != nil {
		return nil, err
	}
	outputW, outputH := baseFrame.presentation.width, baseFrame.presentation.height

	bridgePath := cfg.BridgePath
	if cfg.BridgePath != "" {
		if _, err := os.Stat(bridgePath); os.IsNotExist(err) {
			return nil, err
		}
	} else {
		bridgePath = "./driver_bridge"
	}
	if _, err := os.Stat(bridgePath); os.IsNotExist(err) {
		bridgePath = "/mnt/sdcard/cubegm/driver_bridge"
	}

	args := []string{"--size", fmt.Sprintf("%dx%d", outputW, outputH)}
	if cfg.VSync {
		args = append(args, "--vsync")
	}
	if cfg.UpdateScreen {
		args = append(args, "--update-screen")
	}

	path := cfg.DriverPath
	if path == "" {
		var pathErr error
		path, pathErr = getDriverPath()
		if pathErr != nil {
			path = "/usr/lib/driver.so"
		}
		if _, err := os.Stat(path); err != nil {
			path = "/mnt/sdcard/cubegm/driver_sf3000.so"
		}
	}

	if cfg.DriverPath != "" {
		args = append(args, "--driver", path)
	}
	cmd := exec.Command(bridgePath, args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to open stdin pipe to bridge: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("failed to open stdout pipe to bridge: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("failed to start display bridge: %w", err)
	}

	kill := func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}

	// Handshake: "OK <w> <h> <shm path> <vsync> [R]" or "ERR <message>". The
	// optional R says the bridge understands the row-range command.
	reader := bufio.NewReader(stdout)
	type reply struct {
		line string
		err  error
	}
	replyCh := make(chan reply, 1)
	go func() {
		l, e := reader.ReadString('\n')
		replyCh <- reply{l, e}
	}()

	var line string
	select {
	case r := <-replyCh:
		if r.err != nil {
			kill()
			return nil, fmt.Errorf("failed to read handshake from bridge: %w", r.err)
		}
		line = strings.TrimSpace(r.line)
	case <-time.After(10 * time.Second):
		kill()
		return nil, fmt.Errorf("timeout waiting for the display bridge")
	}

	parts := strings.Fields(line)
	if len(parts) > 0 && parts[0] == "ERR" {
		kill()
		return nil, fmt.Errorf("bridge: %s", strings.TrimSpace(strings.TrimPrefix(line, "ERR")))
	}
	if len(parts) < 5 || parts[0] != "OK" {
		kill()
		return nil, fmt.Errorf("invalid handshake from bridge: %q", line)
	}

	width, errW := strconv.Atoi(parts[1])
	height, errH := strconv.Atoi(parts[2])
	if errW != nil || errH != nil || width <= 0 || height <= 0 {
		kill()
		return nil, fmt.Errorf("invalid dimensions from bridge: %q", line)
	}
	if width != outputW || height != outputH {
		kill()
		return nil, fmt.Errorf("bridge returned %dx%d, expected %dx%d", width, height, outputW, outputH)
	}
	shmPath := parts[3]
	vsyncOK := parts[4] == "1"
	rowsOK := len(parts) >= 6 && parts[5] == "R"

	totalPixels := width * height
	frameSize := totalPixels * 2

	f, err := os.OpenFile(shmPath, os.O_RDWR, 0)
	if err != nil {
		kill()
		return nil, fmt.Errorf("open shared memory %q: %w", shmPath, err)
	}
	shm, err := syscall.Mmap(int(f.Fd()), 0, frameSize,
		syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)

	if err != nil {
		kill()
		return nil, fmt.Errorf("mmap shared memory: %w", err)
	}

	err = f.Close()
	if err != nil {
		kill()
		return nil, fmt.Errorf("closing shared memory: %w", err)
	}

	err = os.Remove(shmPath)
	if err != nil {
		kill()
		return nil, fmt.Errorf("cleaning shared memory: %w", err)
	}

	d := &BridgeDisplayController{
		vsyncActive:  vsyncOK,
		check:        !cfg.DisableFrameCheck,
		rowsOK:       rowsOK,
		pend:         [2]damage{{0, height - 1, 0, width - 1}, {0, height - 1, 0, width - 1}},
		cmd:          cmd,
		stdinPipe:    stdin,
		reader:       reader,
		shm:          shm,
		sharedPixels: unsafe.Slice((*uint16)(unsafe.Pointer(&shm[0])), totalPixels),
	}
	d.frame = baseFrame
	if d.presentation.native(d.w, d.h) {
		d.buf = d.sharedPixels
	}

	if err := d.configureOSD(cfg); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

// Update sends the composed image to the bridge helper.
// Unchanged frames are skipped when frame checking is enabled.
func (d *BridgeDisplayController) Update() error {
	if d.stdinPipe == nil || len(d.buf) == 0 {
		return nil
	}

	t := time.Now()
	dm := d.changed(d.check)
	if d.check {
		if dm.empty() {
			d.lastCopy = time.Since(t)
			d.skipped++
			return nil
		}
	}
	diffTime := time.Since(t)

	if !d.presentation.native(d.w, d.h) {
		var err error
		dm, err = d.presentation.compose(d.sharedPixels, d.buf, d.w, d.h, dm, !d.outputPrimed)
		if err != nil {
			return err
		}
	}
	d.outputPrimed = true

	cmd := bridgeFrameCmd
	if d.rowsOK {
		// The bridge alternates two staging buffers, so the one it fills next
		// is a frame behind and needs the changes of both frames.
		d.pend[0] = d.pend[0].union(dm)
		d.pend[1] = d.pend[1].union(dm)
		rows := d.pend[d.cur]
		d.pend[d.cur] = noDamage
		d.cur ^= 1

		d.rowsCmd = [5]byte{'R', byte(rows.r0), byte(rows.r0 >> 8), byte(rows.r1), byte(rows.r1 >> 8)}
		cmd = d.rowsCmd[:]
	}

	if _, err := d.stdinPipe.Write(cmd); err != nil {
		return err
	}
	if _, err := io.ReadFull(d.reader, d.ack[:]); err != nil {
		return err
	}

	us := func(b []byte) time.Duration {
		return time.Duration(binary.NativeEndian.Uint32(b)) * time.Microsecond
	}
	d.lastCopy = diffTime + us(d.ack[0:4])
	d.lastWait = us(d.ack[4:8])
	d.lastShow = us(d.ack[8:12])
	return nil
}

// Stats returns the active configuration and the latest timings.
func (d *BridgeDisplayController) Stats() Stats {
	return Stats{
		Backend:     "bridge",
		VSyncActive: d.vsyncActive,
		Copy:        d.lastCopy,
		Wait:        d.lastWait,
		Show:        d.lastShow,
		Skipped:     d.skipped,
	}
}

// Close stops the bridge and unmaps the frame buffer. Calling it twice is safe.
func (d *BridgeDisplayController) Close() {
	defer d.closeOSD()
	if d.stdinPipe != nil {
		_ = d.stdinPipe.Close() // EOF makes the bridge shut down
		d.stdinPipe = nil
	}
	if d.cmd != nil {
		// Don't hang if the bridge is stuck inside a driver call.
		cmd := d.cmd
		done := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		d.cmd = nil
	}
	if d.shm != nil {
		_ = syscall.Munmap(d.shm)
		d.shm = nil
		d.sharedPixels = nil
		d.buf = nil
	}
}
