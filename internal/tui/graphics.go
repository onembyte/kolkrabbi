package tui

import (
	_ "embed"
	"encoding/base64"
	"strings"
)

// The owner's supplied purple octopus, extracted onto transparency and reduced
// to 35×32 pixels. Terminals scale it into two columns and one row.
//
//go:embed assets/octopus.png
var octopusPNG []byte

var octopusBase64 = base64.StdEncoding.EncodeToString(octopusPNG)

const (
	deleteOctopus = "\x1b_Ga=d,d=i,i=1263488075,q=2\x1b\\"
	freeOctopus   = "\x1b_Ga=d,d=I,i=1263488075,q=2\x1b\\"
	placeOctopus  = "\x1b_Ga=p,i=1263488075,c=2,r=1,C=1,q=2\x1b\\"
)

var kittyOctopus = "\x1b_Ga=T,f=100,c=2,r=1,C=1,i=1263488075,q=2;" + octopusBase64 + "\x1b\\"
var uploadOctopus = "\x1b_Ga=t,f=100,i=1263488075,q=2;" + octopusBase64 + "\x1b\\"

func renderOctopus(text string, icon, styled bool, graphics []string) string {
	if !icon || !styled || len(graphics) == 0 {
		return text
	}
	paletteMu.RLock()
	colour := activeTier != "none"
	paletteMu.RUnlock()
	if !colour {
		return text
	}
	var sequence string
	switch graphics[0] {
	case "iterm":
		sequence = "\x1b]1337;File=inline=1;width=2;height=1;preserveAspectRatio=1:" + octopusBase64 + "\a"
	case "kitty":
		sequence = kittyOctopus
	default:
		return text
	}
	// Reserve the cells before drawing: writing spaces over an iTerm image
	// would erase it. Save the position after the cells, move back to paint,
	// then restore so both protocols leave subsequent text in the same place.
	return strings.Replace(text, octopusMark, "  \x1b7\x1b[2D"+sequence+"\x1b8", 1)
}
