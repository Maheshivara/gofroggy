//go:build !linux || !mipsle

package display

import "fmt"

// NewGEDisplayController requires the SF3000 Linux/MIPS little-endian ABI.
func NewGEDisplayController(Config) (*GEDisplayController, error) {
	return nil, fmt.Errorf("SF3000 BackendGE requires linux/mipsle (CGO_ENABLED=0 supported)")
}

// GEDisplayController is only implemented for the SF3000.
type GEDisplayController struct{ frame }

func (*GEDisplayController) Update() error { return fmt.Errorf("GE backend unsupported") }
func (*GEDisplayController) Close()        {}
func (*GEDisplayController) Stats() Stats  { return Stats{Backend: "ge"} }
