package pi

import "strings"

// isValidXMLText 检查字符串中的所有字符是否都允许出现在 XML 1.0 文本中。
func isValidXMLText(value string) bool {
	for _, character := range value {
		if !isValidXMLCharacter(character) {
			return false
		}
	}
	return true
}

// sanitizeXMLText 将 XML 1.0 不允许的字符替换为 Unicode Replacement Character，
// 避免生成的 Skill 目录成为无效 XML。
func sanitizeXMLText(value string) string {
	return strings.Map(func(character rune) rune {
		if isValidXMLCharacter(character) {
			return character
		}
		return '\uFFFD'
	}, value)
}

// isValidXMLCharacter 判断单个 Unicode 字符是否属于 XML 1.0 允许的字符范围。
func isValidXMLCharacter(character rune) bool {
	return character == '\t' || character == '\n' || character == '\r' ||
		character >= 0x20 && character <= 0xD7FF ||
		character >= 0xE000 && character <= 0xFFFD ||
		character >= 0x10000 && character <= 0x10FFFF
}
