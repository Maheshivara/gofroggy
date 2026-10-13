package mapping

// InputButton identifies a device button.
type InputButton uint8

// The device buttons.
const (
	SELECT InputButton = iota
	START
	UP
	RIGHT
	DOWN
	LEFT
	L2
	R2
	L1
	R1
	X
	A
	B
	Y
)

// InputButtonMask is one bit of the raw button state.
type InputButtonMask uint16

// ONE to SIXTEEN are bits 0 to 15 of the raw button state.
const (
	ONE      InputButtonMask = 1 << 0
	TWO      InputButtonMask = 1 << 1
	THREE    InputButtonMask = 1 << 2
	FOUR     InputButtonMask = 1 << 3
	FIVE     InputButtonMask = 1 << 4
	SIX      InputButtonMask = 1 << 5
	SEVEN    InputButtonMask = 1 << 6
	EIGHT    InputButtonMask = 1 << 7
	NINE     InputButtonMask = 1 << 8
	TEN      InputButtonMask = 1 << 9
	ELEVEN   InputButtonMask = 1 << 10
	TWELVE   InputButtonMask = 1 << 11
	THIRTEEN InputButtonMask = 1 << 12
	FOURTEEN InputButtonMask = 1 << 13
	FIFTEEN  InputButtonMask = 1 << 14
	SIXTEEN  InputButtonMask = 1 << 15
)
