package mapping

import "fmt"

// InputMapping assigns a raw-state bit to each button; each bit may be used once.
type InputMapping struct {
	// Each field is the bit of that button; 0 means unmapped.
	BtnSelect InputButtonMask
	BtnStart  InputButtonMask
	BtnUp     InputButtonMask
	BtnRight  InputButtonMask
	BtnDown   InputButtonMask
	BtnLeft   InputButtonMask
	BtnL2     InputButtonMask
	BtnR2     InputButtonMask
	BtnL1     InputButtonMask
	BtnR1     InputButtonMask
	BtnX      InputButtonMask
	BtnA      InputButtonMask
	BtnB      InputButtonMask
	BtnY      InputButtonMask
}

// DefaultInputMapping is the standard button layout of the device.
var DefaultInputMapping *InputMapping = &InputMapping{
	BtnSelect: ONE,
	BtnStart:  FOUR,
	BtnUp:     FIVE,
	BtnRight:  SIX,
	BtnDown:   SEVEN,
	BtnLeft:   EIGHT,
	BtnL2:     NINE,
	BtnR2:     TEN,
	BtnL1:     ELEVEN,
	BtnR1:     TWELVE,
	BtnX:      THIRTEEN,
	BtnA:      FOURTEEN,
	BtnB:      FIFTEEN,
	BtnY:      SIXTEEN,
}

// GetButtonMap returns the mapping as a map from button to bit.
func (i *InputMapping) GetButtonMap() map[InputButton]InputButtonMask {
	btnMap := make(map[InputButton]InputButtonMask, 14)

	btnMap[SELECT] = i.BtnSelect
	btnMap[START] = i.BtnStart
	btnMap[UP] = i.BtnUp
	btnMap[RIGHT] = i.BtnRight
	btnMap[DOWN] = i.BtnDown
	btnMap[LEFT] = i.BtnLeft
	btnMap[L2] = i.BtnL2
	btnMap[R2] = i.BtnR2
	btnMap[L1] = i.BtnL1
	btnMap[R1] = i.BtnR1
	btnMap[X] = i.BtnX
	btnMap[A] = i.BtnA
	btnMap[B] = i.BtnB
	btnMap[Y] = i.BtnY

	return btnMap
}

// isEmpty reports whether no button is mapped.
func (m *InputMapping) isEmpty() bool {
	return m.BtnSelect == 0 && m.BtnStart == 0 &&
		m.BtnUp == 0 && m.BtnRight == 0 && m.BtnDown == 0 && m.BtnLeft == 0 &&
		m.BtnL2 == 0 && m.BtnR2 == 0 && m.BtnL1 == 0 && m.BtnR1 == 0 &&
		m.BtnX == 0 && m.BtnA == 0 && m.BtnB == 0 && m.BtnY == 0
}

// checkInputs returns an error if two buttons share a bit.
func (m *InputMapping) checkInputs() error {
	seen := make(map[InputButtonMask]string)

	fields := []struct {
		name  string
		value InputButtonMask
	}{
		{"BtnSelect", m.BtnSelect},
		{"BtnStart", m.BtnStart},
		{"BtnUp", m.BtnUp},
		{"BtnRight", m.BtnRight},
		{"BtnDown", m.BtnDown},
		{"BtnLeft", m.BtnLeft},
		{"BtnL2", m.BtnL2},
		{"BtnR2", m.BtnR2},
		{"BtnL1", m.BtnL1},
		{"BtnR1", m.BtnR1},
		{"BtnX", m.BtnX},
		{"BtnA", m.BtnA},
		{"BtnB", m.BtnB},
		{"BtnY", m.BtnY},
	}

	for _, field := range fields {
		if field.value == 0 {
			continue
		}

		if existing, exists := seen[field.value]; exists {
			return fmt.Errorf("duplicate key mapping found: code %d is assigned to both %s and %s", field.value, existing, field.name)
		}
		seen[field.value] = field.name
	}

	return nil
}

// Validate returns an error if no button is mapped or two buttons share a bit.
func (m *InputMapping) Validate() error {
	empty := m.isEmpty()
	if empty {
		return fmt.Errorf("input mapping has no set buttons")
	}

	return m.checkInputs()
}
