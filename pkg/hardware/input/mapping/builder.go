package mapping

// InputMappingBuilder builds an InputMapping with chained WithBtn calls.
type InputMappingBuilder struct {
	btnSelect InputButtonMask
	btnStart  InputButtonMask
	btnUp     InputButtonMask
	btnRight  InputButtonMask
	btnDown   InputButtonMask
	btnLeft   InputButtonMask
	btnL2     InputButtonMask
	btnR2     InputButtonMask
	btnL1     InputButtonMask
	btnR1     InputButtonMask
	btnX      InputButtonMask
	btnA      InputButtonMask
	btnB      InputButtonMask
	btnY      InputButtonMask
}

// NewInputMappingBuilder returns an empty builder.
func NewInputMappingBuilder() *InputMappingBuilder {
	return &InputMappingBuilder{}
}

// WithBtnA sets the bit for A.
func (c *InputMappingBuilder) WithBtnA(m InputButtonMask) *InputMappingBuilder {
	c.btnA = m
	return c
}

// WithBtnB sets the bit for B.
func (c *InputMappingBuilder) WithBtnB(m InputButtonMask) *InputMappingBuilder {
	c.btnB = m
	return c
}

// WithBtnX sets the bit for X.
func (c *InputMappingBuilder) WithBtnX(m InputButtonMask) *InputMappingBuilder {
	c.btnX = m
	return c
}

// WithBtnY sets the bit for Y.
func (c *InputMappingBuilder) WithBtnY(m InputButtonMask) *InputMappingBuilder {
	c.btnY = m
	return c
}

// WithBtnSelect sets the bit for SELECT.
func (c *InputMappingBuilder) WithBtnSelect(m InputButtonMask) *InputMappingBuilder {
	c.btnSelect = m
	return c
}

// WithBtnStart sets the bit for START.
func (c *InputMappingBuilder) WithBtnStart(m InputButtonMask) *InputMappingBuilder {
	c.btnStart = m
	return c
}

// WithBtnUp sets the bit for UP.
func (c *InputMappingBuilder) WithBtnUp(m InputButtonMask) *InputMappingBuilder {
	c.btnUp = m
	return c
}

// WithBtnDown sets the bit for DOWN.
func (c *InputMappingBuilder) WithBtnDown(m InputButtonMask) *InputMappingBuilder {
	c.btnDown = m
	return c
}

// WithBtnLeft sets the bit for LEFT.
func (c *InputMappingBuilder) WithBtnLeft(m InputButtonMask) *InputMappingBuilder {
	c.btnLeft = m
	return c
}

// WithBtnRight sets the bit for RIGHT.
func (c *InputMappingBuilder) WithBtnRight(m InputButtonMask) *InputMappingBuilder {
	c.btnRight = m
	return c
}

// WithBtnR1 sets the bit for R1.
func (c *InputMappingBuilder) WithBtnR1(m InputButtonMask) *InputMappingBuilder {
	c.btnR1 = m
	return c
}

// WithBtnL1 sets the bit for L1.
func (c *InputMappingBuilder) WithBtnL1(m InputButtonMask) *InputMappingBuilder {
	c.btnL1 = m
	return c
}

// WithBtnR2 sets the bit for R2.
func (c *InputMappingBuilder) WithBtnR2(m InputButtonMask) *InputMappingBuilder {
	c.btnR2 = m
	return c
}

// WithBtnL2 sets the bit for L2.
func (c *InputMappingBuilder) WithBtnL2(m InputButtonMask) *InputMappingBuilder {
	c.btnL1 = m
	return c
}

// Build returns the mapping, or an error if it is invalid (see Validate).
func (c *InputMappingBuilder) Build() (*InputMapping, error) {
	newMapping := &InputMapping{
		BtnSelect: c.btnSelect,
		BtnStart:  c.btnStart,
		BtnUp:     c.btnUp,
		BtnRight:  c.btnRight,
		BtnDown:   c.btnDown,
		BtnLeft:   c.btnLeft,
		BtnL2:     c.btnL2,
		BtnR2:     c.btnR2,
		BtnL1:     c.btnL1,
		BtnR1:     c.btnR1,
		BtnX:      c.btnX,
		BtnA:      c.btnA,
		BtnB:      c.btnB,
		BtnY:      c.btnY,
	}

	err := newMapping.Validate()
	if err != nil {
		return nil, err
	}

	return newMapping, nil
}
