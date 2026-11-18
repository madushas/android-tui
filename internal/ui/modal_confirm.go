package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type confirmModal struct {
	msg    string
	danger bool
	yes    cont
	// button hit-boxes, set by view (columns relative to the box, row = box row)
	btnRow, yesA, yesB, noA, noB int
}

// mouse: click [ Yes ] or [ No ]. Clicking elsewhere does nothing, so a stray click can never confirm or cancel.
func (c *confirmModal) mouse(m *Model, ev mouseEv, box rect) (bool, cont) {
	if (ev.kind != mClick && ev.kind != mDouble) || ev.y != box.y+c.btnRow {
		return false, nil
	}
	switch x := ev.x - box.x; {
	case x >= c.yesA && x < c.yesB:
		return true, c.yes
	case x >= c.noA && x < c.noB:
		return true, nil
	}
	return false, nil
}

func (c *confirmModal) update(m *Model, msg tea.Msg) (bool, cont) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch s := k.String(); {
		case s == "y" || s == "Y" || (s == "enter" && !c.danger):
			return true, c.yes
		case s == "n" || s == "N" || s == "esc" || s == "q" || s == "enter" || s == "ctrl+c":
			return true, nil
		}
	}
	return false, nil
}

func (c *confirmModal) view(w, h int) []string {
	iw := min(w-6, 62)
	var body []string
	for _, para := range strings.Split(c.msg, "\n") {
		for _, l := range wrap(para, iw-2) {
			body = append(body, " "+l)
		}
	}
	if c.danger && len(body) > 0 {
		body[0] = sErr.Render(body[0])
	}
	yes, no := "[ Yes  y ]", "[ No  n ]"
	yesSt := sOK
	if c.danger {
		yesSt = sErr
	}
	hint := "   Enter = yes"
	if c.danger {
		hint = "   Enter = no"
	}
	body = append(body, "", " "+yesSt.Render(yes)+"   "+sBold.Render(no)+sDim.Render(hint))
	c.btnRow = len(body) // box row of the button line (the frame's top border is row 0)
	c.yesA = 2
	c.yesB = c.yesA + sw(yes)
	c.noA = c.yesB + 3
	c.noB = c.noA + sw(no)
	t := " Confirm "
	if c.danger {
		t = " Danger "
	}
	return frame(t, padAll(body, iw), iw+2, "", c.danger)
}

// typedConfirmModal is for catastrophic actions (wipe, delete, revert-all): the word must be typed before Enter works.
type typedConfirmModal struct {
	msg, word, buf string
	yes            cont
}

func (c *typedConfirmModal) update(m *Model, msg tea.Msg) (bool, cont) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}
	switch s := k.String(); {
	case s == "esc" || s == "ctrl+c":
		return true, nil
	case s == "enter":
		if strings.EqualFold(strings.TrimSpace(c.buf), c.word) {
			return true, c.yes
		}
	case s == "backspace":
		if r := []rune(c.buf); len(r) > 0 {
			c.buf = string(r[:len(r)-1])
		}
	case k.Type == tea.KeyRunes:
		c.buf += string(k.Runes)
	}
	return false, nil
}

func (c *typedConfirmModal) view(w, h int) []string {
	iw := min(w-6, 62)
	var body []string
	for _, para := range strings.Split(c.msg, "\n") {
		for _, l := range wrap(para, iw-2) {
			body = append(body, " "+l)
		}
	}
	if len(body) > 0 {
		body[0] = sErr.Render(body[0])
	}
	state := sDim.Render("(type it to enable Enter)")
	if strings.EqualFold(strings.TrimSpace(c.buf), c.word) {
		state = sOK.Render("✓ Enter to confirm")
	}
	body = append(body, "", " Type "+sErrB.Render(strings.ToUpper(c.word))+" to confirm:", " > "+c.buf+sRev.Render(" ")+"  "+state, "", " "+sDim.Render("Esc cancels"))
	return frame(" Danger ", padAll(body, iw), iw+2, "", true)
}
