package input

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unsafe"

	"github.com/Maheshivara/gofroggy/pkg/hardware/input/mapping"
)

// InputController reads the device buttons from the cubevol process.
type InputController struct {
	memFile       *os.File
	addrs         []uintptr
	state         uint16
	last          uint16
	buttonMapping map[mapping.InputButton]mapping.InputButtonMask
}

// findCubevolPID returns the PID of the cubevol process.
func findCubevolPID() (int, error) {
	files, err := os.ReadDir("/proc")
	if err != nil {
		return 0, err
	}

	for _, file := range files {
		if !file.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(file.Name())
		if err != nil {
			continue
		}

		cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		if err == nil && strings.Contains(string(cmdline), "cubevol") {
			return pid, nil
		}
	}
	return 0, fmt.Errorf("cubevol process not found")
}

// findCubevolShmCandidateAddresses returns the start of every shared (rw-s)
// memory region of pid.
func findCubevolShmCandidateAddresses(pid int) ([]uintptr, error) {
	mapsFile, err := os.Open(fmt.Sprintf("/proc/%d/maps", pid))
	if err != nil {
		return nil, err
	}
	defer func() {
		err = mapsFile.Close()
	}()

	var candidates []uintptr
	scanner := bufio.NewScanner(mapsFile)
	for scanner.Scan() {
		line := scanner.Text()

		if strings.Contains(line, "rw-s") {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				addrs := strings.Split(fields[0], "-")
				if len(addrs) == 2 {
					addr, err := strconv.ParseUint(addrs[0], 16, 32)
					if err == nil {
						candidates = append(candidates, uintptr(addr))
					}
				}
			}
		}
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no rw-s candidate memory regions found in cubevol")
	}

	return candidates, err
}

// NewInputController attaches to cubevol. A nil or invalid m selects
// DefaultInputMapping.
func NewInputController(m *mapping.InputMapping) (*InputController, error) {
	pid, err := findCubevolPID()
	if err != nil {
		return nil, err
	}

	addrs, err := findCubevolShmCandidateAddresses(pid)
	if err != nil {
		return nil, err
	}

	memFile, err := os.OpenFile(fmt.Sprintf("/proc/%d/mem", pid), os.O_RDONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open /proc/%d/mem: %v", pid, err)
	}
	var inputMapping mapping.InputMapping
	if m == nil || m.Validate() != nil {
		inputMapping = *mapping.DefaultInputMapping
	} else {
		inputMapping = *m
	}

	return &InputController{
		memFile:       memFile,
		addrs:         addrs,
		buttonMapping: inputMapping.GetButtonMap(),
	}, nil
}

// Update reads the current button state. Call it once per frame.
func (in *InputController) Update() {
	in.last = in.state
	if in.memFile != nil && len(in.addrs) > 0 {
		var combinedState uint16 = 0
		var buf [4]byte

		for _, addr := range in.addrs {
			_, err := in.memFile.ReadAt(buf[:], int64(addr))
			if err == nil {
				val := *(*uint16)(unsafe.Pointer(&buf[0])) & 0xFFFF
				if val != 0 {
					combinedState |= val
				}
			}
		}
		in.state = combinedState
	}
}

// Remap replaces the button mapping; it returns an error if m is invalid.
func (in *InputController) Remap(m *mapping.InputMapping) error {
	err := m.Validate()
	if err != nil {
		return err
	}

	in.buttonMapping = m.GetButtonMap()
	return nil
}

// IsPressed reports whether b is held, as of the last Update.
func (in *InputController) IsPressed(b mapping.InputButton) bool {
	return (in.state & uint16(in.buttonMapping[b])) != 0
}

// IsJustPressed reports whether b went down since the previous Update.
func (in *InputController) IsJustPressed(b mapping.InputButton) bool {
	return (in.state&uint16(in.buttonMapping[b])) != 0 && (in.last&uint16(in.buttonMapping[b])) == 0
}

// Close releases the cubevol memory handle.
func (in *InputController) Close() error {
	if in.memFile != nil {
		err := in.memFile.Close()
		return err
	}
	return nil
}
