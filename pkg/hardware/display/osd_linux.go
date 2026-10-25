//go:build linux

package display

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const (
	osdFramebuffer = "/dev/fb1"
	osdBlankState  = "/sys/class/graphics/fb1/blank"
	osdFBGetFixed  = 0x4602
	osdFBBlank     = 0x4611
)

type nativeOSDDevice struct {
	fd int
}

// openNativeOSDDevice validates the secondary framebuffer before modifying it.
func openNativeOSDDevice() (osdDevice, error) {
	fd, err := syscall.Open(osdFramebuffer, syscall.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s (OSD layer): %w", osdFramebuffer, err)
	}
	var info [128]byte
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), osdFBGetFixed, uintptr(unsafe.Pointer(&info[0])))
	if errno != 0 {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("%s is not a readable framebuffer (FBIOGET_FSCREENINFO): %w", osdFramebuffer, errno)
	}
	return &nativeOSDDevice{fd: fd}, nil
}

// blankState reads the sysfs state when supplied by the kernel.
func (d *nativeOSDDevice) blankState() (int, bool) {
	b, err := os.ReadFile(osdBlankState)
	if err != nil {
		return 0, false
	}
	i, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || i < 0 || i > 4 {
		return 0, false
	}
	return i, true
}

// setBlank targets fb1 only; normal blank must not power down the panel.
func (d *nativeOSDDevice) setBlank(level int) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(d.fd), osdFBBlank, uintptr(level))
	if errno != 0 {
		return errno
	}
	return nil
}

func (d *nativeOSDDevice) close() error { return syscall.Close(d.fd) }
