package tokens

type Position string

const (
	PositionOff   Position = "off"
	PositionStart Position = "start"
	PositionEnd   Position = "end"
)

func (p Position) Valid() bool {
	switch p {
	case PositionOff, PositionStart, PositionEnd:
		return true
	default:
		return false
	}
}

type Display struct {
	Position Position
	Value    string
}
