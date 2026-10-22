package presentation

import "unicode/utf16"

// MessageLimit is Discord's visible message budget in UTF-16 units.
const MessageLimit = 2000

func messageSize(text string) int {
	return len(utf16.Encode([]rune(text)))
}
