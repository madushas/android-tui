package core

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type xNode struct {
	Class    string  `xml:"class,attr"`
	Text     string  `xml:"text,attr"`
	ResID    string  `xml:"resource-id,attr"`
	Desc     string  `xml:"content-desc,attr"`
	Pkg      string  `xml:"package,attr"`
	Click    string  `xml:"clickable,attr"`
	Scroll   string  `xml:"scrollable,attr"`
	Checked  string  `xml:"checked,attr"`
	Enabled  string  `xml:"enabled,attr"`
	Bounds   string  `xml:"bounds,attr"`
	Children []xNode `xml:"node"`
}

type xRoot struct {
	Nodes []xNode `xml:"node"`
}

// UINode is a simplified view-hierarchy node.
type UINode struct {
	Class, Text, ID, Desc, Pkg              string
	Clickable, Scrollable, Checked, Enabled bool
	Bounds                                  [4]int
	Children                                []*UINode `json:",omitempty"`
}

var boundsRE = regexp.MustCompile(`\[(\d+),(\d+)\]\[(\d+),(\d+)\]`)

func conv(x xNode) *UINode {
	n := &UINode{Text: x.Text, Desc: x.Desc, Pkg: x.Pkg, Clickable: x.Click == "true", Scrollable: x.Scroll == "true", Checked: x.Checked == "true", Enabled: x.Enabled != "false"}
	n.Class = x.Class
	if i := strings.LastIndex(n.Class, "."); i >= 0 {
		n.Class = n.Class[i+1:]
	}
	if n.Class == "" {
		n.Class = "node"
	}
	n.ID = x.ResID
	if i := strings.Index(n.ID, ":id/"); i >= 0 {
		n.ID = n.ID[i+4:]
	}
	if m := boundsRE.FindStringSubmatch(x.Bounds); m != nil {
		for i := 0; i < 4; i++ {
			n.Bounds[i], _ = strconv.Atoi(m[i+1])
		}
	}
	for _, c := range x.Children {
		n.Children = append(n.Children, conv(c))
	}
	return n
}

// ParseUIDump parses uiautomator XML into a single root node.
func ParseUIDump(xmlText string) (*UINode, error) {
	i := strings.Index(xmlText, "<?xml")
	if i < 0 {
		i = strings.Index(xmlText, "<hierarchy")
	}
	if i < 0 {
		return nil, fmt.Errorf("no UI hierarchy in output: %s", Clip(strings.TrimSpace(xmlText), 120))
	}
	var r xRoot
	if err := xml.Unmarshal([]byte(xmlText[i:]), &r); err != nil {
		return nil, err
	}
	root := &UINode{Class: "hierarchy"}
	for _, n := range r.Nodes {
		root.Children = append(root.Children, conv(n))
	}
	if len(root.Children) == 1 {
		return root.Children[0], nil
	}
	return root, nil
}

func UIDump(serial string) (*UINode, error) {
	r := Sh(serial, "uiautomator dump /data/local/tmp/uidump.xml >/dev/null 2>&1; cat /data/local/tmp/uidump.xml; rm -f /data/local/tmp/uidump.xml", Opt{Timeout: 40 * time.Second})
	return ParseUIDump(r.Out)
}

func (n *UINode) informative() bool { return n.Text != "" || n.Desc != "" || n.ID != "" || n.Clickable }

func (n *UINode) label(bounds bool) string {
	s := "[" + n.Class + "]"
	if n.Text != "" {
		s += fmt.Sprintf(" text=%q", n.Text)
	}
	if n.ID != "" {
		s += fmt.Sprintf(" id=%q", n.ID)
	}
	if n.Desc != "" {
		s += fmt.Sprintf(" desc=%q", n.Desc)
	}
	if n.Clickable {
		s += " (clickable: true)"
	} else if n.Text != "" || n.ID != "" {
		s += " (clickable: false)"
	}
	if n.Checked {
		s += " (checked)"
	}
	if n.Scrollable {
		s += " (scrollable)"
	}
	if bounds && n.Bounds != [4]int{} {
		s += fmt.Sprintf(" bounds=[%d,%d][%d,%d]", n.Bounds[0], n.Bounds[1], n.Bounds[2], n.Bounds[3])
	}
	return s
}

// flatten hoists children of uninformative nodes when compact.
func (n *UINode) kids(compact bool) []*UINode {
	var out []*UINode
	for _, c := range n.Children {
		if compact && !c.informative() {
			out = append(out, c.kids(true)...)
		} else {
			out = append(out, c)
		}
	}
	return out
}

// RenderTree draws the hierarchy with box-drawing connectors.
func (n *UINode) RenderTree(compact, bounds bool) []string {
	out := []string{n.label(bounds)}
	var walk func(x *UINode, prefix string)
	walk = func(x *UINode, prefix string) {
		ks := x.kids(compact)
		for i, c := range ks {
			conn, next := "├── ", "│   "
			if i == len(ks)-1 {
				conn, next = "└── ", "    "
			}
			out = append(out, prefix+conn+c.label(bounds))
			walk(c, prefix+next)
		}
	}
	walk(n, "")
	return out
}

func (n *UINode) RenderMarkdown(compact bool) string {
	var sb strings.Builder
	var walk func(x *UINode, depth int)
	walk = func(x *UINode, depth int) {
		fmt.Fprintf(&sb, "%s- %s\n", strings.Repeat("  ", depth), x.label(true))
		for _, c := range x.kids(compact) {
			walk(c, depth+1)
		}
	}
	walk(n, 0)
	return sb.String()
}

func (n *UINode) RenderJSON(compact bool) string {
	var conv func(x *UINode) map[string]any
	conv = func(x *UINode) map[string]any {
		m := map[string]any{"class": x.Class}
		if x.Text != "" {
			m["text"] = x.Text
		}
		if x.ID != "" {
			m["id"] = x.ID
		}
		if x.Desc != "" {
			m["desc"] = x.Desc
		}
		if x.Clickable {
			m["clickable"] = true
		}
		if x.Bounds != [4]int{} {
			m["bounds"] = x.Bounds
		}
		var ks []map[string]any
		for _, c := range x.kids(compact) {
			ks = append(ks, conv(c))
		}
		if len(ks) > 0 {
			m["children"] = ks
		}
		return m
	}
	b, _ := json.MarshalIndent(conv(n), "", "  ")
	return string(b)
}

// Targets lists tappable/informative leaf-ish nodes (clickable, or with text), for the "tap element" picker.
func (n *UINode) Targets() []*UINode {
	var out []*UINode
	var walk func(x *UINode)
	walk = func(x *UINode) {
		if (x.Clickable || x.Text != "") && x.Bounds != [4]int{} && (x.Text != "" || x.Desc != "" || x.ID != "") {
			out = append(out, x)
		}
		for _, c := range x.Children {
			walk(c)
		}
	}
	walk(n)
	return out
}

func (n *UINode) Center() (int, int) {
	return (n.Bounds[0] + n.Bounds[2]) / 2, (n.Bounds[1] + n.Bounds[3]) / 2
}

// ───────────────────────────── processes (top + meminfo) ─────────────────────────────
