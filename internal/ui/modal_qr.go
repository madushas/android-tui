package ui

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
	"github.com/madushas/android-tui/internal/qr"
)

type qrModal struct {
	q       *core.QRPair
	qr      []string
	then    func(m *Model, ok bool) tea.Cmd
	lines   []string
	fin, ok bool
}

func newQRModal(then func(m *Model, ok bool) tea.Cmd) (*qrModal, error) {
	q, err := core.NewQRPair()
	if err != nil {
		return nil, err
	}
	mat, err := qr.Encode(q.Payload)
	if err != nil {
		return nil, err
	}
	go q.Run()
	return &qrModal{q: q, qr: qrRender(mat), then: then}, nil
}

func (q *qrModal) update(m *Model, msg tea.Msg) (bool, cont) {
	switch msg := msg.(type) {
	case tickMsg:
		q.lines, q.fin, q.ok = q.q.State()
	case tea.KeyMsg:
		if q.fin || msg.String() == "esc" || msg.String() == "ctrl+c" {
			q.q.Cancel()
			ok := q.ok
			return true, func(m *Model) tea.Cmd { return q.then(m, ok) }
		}
	}
	return false, nil
}

func (q *qrModal) view(w, h int) []string {
	qw := sw(q.qr[0])
	iw := min(w-4, max(qw+6, 62))
	need := len(q.qr) + 9
	if h < need+2 {
		return frame(" QR pairing ", padAll([]string{" " + sErr.Render(fmt.Sprintf("terminal too short — need %d rows (have %d). Esc cancels.", need+2, h))}, iw), iw+2, "", false)
	}
	body := []string{" " + sDim.Render("Phone: Settings → Developer options → Wireless debugging →"), " " + sDim.Render("Pair device with QR code, then scan:")}
	for _, l := range q.qr {
		body = append(body, fit(strings.Repeat(" ", (iw-qw)/2)+sQR.Render(l), iw))
	}
	ls := q.lines
	if len(ls) > 4 {
		ls = ls[len(ls)-4:]
	}
	for _, l := range ls {
		st := sNone
		if strings.Contains(l, "✓") {
			st = sOKB
		} else if strings.Contains(l, "timed out") {
			st = sErr
		}
		body = append(body, " "+st.Render(clipS(l, iw-2)))
	}
	for len(body) < len(q.qr)+8 {
		body = append(body, "")
	}
	if q.fin {
		body = append(body, " "+sDim.Render("any key closes"))
	} else {
		body = append(body, " "+sAcc.Render(spinner(int(time.Now().UnixMilli()/100)))+sDim.Render(" listening via mDNS · Esc cancels"))
	}
	return frame(" Pair with QR code ", padAll(body, iw), iw+2, "", false)
}

// qrRender draws the symbol with the 4-module quiet zone the QR specification requires, black on white whatever the
// terminal theme is: a dark theme would otherwise invert the code, which many phone scanners reject.
func qrRender(mat [][]bool) []string {
	st := lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("15"))
	lines := qr.Lines(mat, 4)
	for i, l := range lines {
		lines[i] = st.Render(l)
	}
	return lines
}
