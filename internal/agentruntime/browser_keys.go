package agentruntime

// BrowserKey returns bounded navigation/game keys, never shortcuts or arbitrary text.
func BrowserKey(text string) (key, code string, virtual int, ok bool) {
	switch text {
	case "Tab":
		return text, text, 9, true
	case "Enter":
		return text, text, 13, true
	case "Escape":
		return text, text, 27, true
	case "ArrowLeft":
		return text, text, 37, true
	case "ArrowUp":
		return text, text, 38, true
	case "ArrowRight":
		return text, text, 39, true
	case "ArrowDown":
		return text, text, 40, true
	case "Space":
		return " ", "Space", 32, true
	case "Shift":
		return "Shift", "ShiftLeft", 16, true
	case "w", "a", "s", "d", "e", "q":
		return text, "Key" + string([]byte{text[0] - 32}), int(text[0] - 32), true
	}
	return "", "", 0, false
}
