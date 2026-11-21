package tui

// helixHelpLines document the helix engine in the in-app manual, after the
// shared sections.
var helixHelpLines = []string{
	"",
	"## Helix motions",
	"[vim] motions\tThe editing engine: vim (operator then motion, the default) or helix (select first, then act). It applies to notes opened afterwards",
	"h j k l 0 ^ $\tMoves; word and find motions select as they travel",
	"w b e W B E\tSelect a whole word and the space after it; b takes the word it lands on",
	"f t F T , ;\tSelect through a character; ; also collapses the selection",
	"n N * # / ?\tSelect the next match, use the selection as the pattern, search",
	"gg G { } H M L\tFile, paragraph and screen motions",
	"v\tSelect mode: motions extend the selection",
	"x\tSelect the line; again to extend",
	"d c y\tDelete, change or yank the selection",
	"mi ma\tSelect inside or around a text object (miw, ma\")",
	"Alt+O / Alt+Up\tGrow the selection: word, paragraph, code function, section, note",
	"Alt+I / Alt+Down\tShrink the selection back to the smaller context",
	"zf zo za zM zR\tFold, unfold, toggle, fold all, unfold all headings",
	"gn gp\tNext and previous tab",
	"[keys.normal] [keys.select] [keys.insert]\tRebind the helix engine's keys in Helix's own config format (docs/helix.md)",
}
