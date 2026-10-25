package display

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// osdDevice controls the system framebuffer overlay, not application pixels.
type osdDevice interface {
	blankState() (int, bool)
	setBlank(int) error
	close() error
}

var systemOSD struct {
	sync.Mutex
	dev   osdDevice
	users int
	prior int
	stop  chan struct{}
}

// openOSDDevice is platform-specific; tests can replace it with a fake.
var openOSDDevice = openNativeOSDDevice

type osdLease struct {
	once sync.Once
}

// acquireOSD hides the shared fb1 overlay until the final owner closes.
func acquireOSD() (*osdLease, error) {
	systemOSD.Lock()
	defer systemOSD.Unlock()
	if systemOSD.users != 0 {
		systemOSD.users++
		return &osdLease{}, nil
	}
	dev, err := openOSDDevice()
	if err != nil {
		return nil, err
	}
	prior, known := dev.blankState()
	if !known {
		prior = 0 // FBIOBLANK has no standard get-state ioctl.
	}
	if err := dev.setBlank(1); err != nil {
		_ = dev.close()
		return nil, fmt.Errorf("blank system OSD: %w", err)
	}
	systemOSD.dev = dev
	systemOSD.users = 1
	systemOSD.prior = prior
	systemOSD.stop = make(chan struct{})
	go keepOSDBlanked(systemOSD.stop)
	return &osdLease{}, nil
}

// keepOSDBlanked counters stock software re-enabling the OSD layer.
func keepOSDBlanked(stop <-chan struct{}) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			systemOSD.Lock()
			if systemOSD.dev != nil && systemOSD.stop == stop {
				if err := systemOSD.dev.setBlank(1); err != nil {
					fmt.Fprintf(os.Stderr, "display: reblank /dev/fb1 OSD: %v\n", err)
				}
			}
			systemOSD.Unlock()
		}
	}
}

// Close restores the prior blank state after the last user exits.
func (l *osdLease) Close() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		systemOSD.Lock()
		defer systemOSD.Unlock()
		if systemOSD.users == 0 {
			return
		}
		systemOSD.users--
		if systemOSD.users != 0 {
			return
		}
		close(systemOSD.stop)
		state := systemOSD.prior
		if err := systemOSD.dev.setBlank(state); err != nil {
			fmt.Fprintf(os.Stderr, "display: restore /dev/fb1 OSD blank=%d: %v\n", state, err)
		}
		if err := systemOSD.dev.close(); err != nil {
			fmt.Fprintf(os.Stderr, "display: close /dev/fb1: %v\n", err)
		}
		systemOSD.dev = nil
		systemOSD.stop = nil
	})
}

// configureOSD applies the optional display-independent system OSD policy.
func (f *frame) configureOSD(cfg Config) error {
	if !cfg.HideSystemOSD {
		return nil
	}
	lease, err := acquireOSD()
	if err != nil {
		return fmt.Errorf("HideSystemOSD: %w", err)
	}
	f.osd = lease
	return nil
}

// closeOSD releases the shared overlay policy, leaving input daemons running.
func (f *frame) closeOSD() {
	if f.osd != nil {
		f.osd.Close()
		f.osd = nil
	}
}
