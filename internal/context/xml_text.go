package context

import "strings"

func isValidXMLText(value string) bool {
	for _, character := range value {
		if !isValidXMLCharacter(character) {
			return false
		}
	}
	return true
}

func sanitizeXMLText(value string) string {
	return strings.Map(func(character rune) rune {
		if isValidXMLCharacter(character) {
			return character
		}
		return '\uFFFD'
	}, value)
}

func isValidXMLCharacter(character rune) bool {
	return character == '\t' || character == '\n' || character == '\r' ||
		character >= 0x20 && character <= 0xD7FF ||
		character >= 0xE000 && character <= 0xFFFD ||
		character >= 0x10000 && character <= 0x10FFFF
}
