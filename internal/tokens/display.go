package tokens

import "strings"

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

func IsDisplayValue(value string) bool {
	unit := byte(0)
	if len(value) > 0 && strings.ContainsRune("kmbt", rune(value[len(value)-1])) {
		unit = value[len(value)-1]
		value = value[:len(value)-1]
	}
	if value == "" || value[0] == '0' && value != "0" {
		return false
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || !decimalDigits(parts[0]) {
		return false
	}
	if len(parts) == 2 {
		return unit != 0 && len(parts[0]) == 1 && len(parts[1]) == 1 && parts[1][0] >= '1' && parts[1][0] <= '9'
	}
	if unit == 0 {
		return len(parts[0]) <= 3
	}
	return parts[0] != "0" && (unit == 't' || len(parts[0]) <= 3)
}

func decimalDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}
