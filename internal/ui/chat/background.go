package chat

import (
	"fmt"
	"image/color"
	"strings"
)

const ansiColorChannelShift = 8

// keepBackground restores a container fill after nested styles reset their
// ANSI state. Content owns its foreground formatting, while its container
// owns the background of every cell.
func keepBackground(content string, background color.Color) string {
	red, green, blue, _ := background.RGBA()
	fill := fmt.Sprintf("\x1b[48;2;%d;%d;%dm", red>>ansiColorChannelShift, green>>ansiColorChannelShift, blue>>ansiColorChannelShift)
	replacer := strings.NewReplacer(
		"\x1b[m", "\x1b[m"+fill,
		"\x1b[0m", "\x1b[0m"+fill,
		"\x1b[49m", "\x1b[49m"+fill,
	)
	return replacer.Replace(content)
}
