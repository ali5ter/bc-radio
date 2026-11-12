package bandcamp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"golang.org/x/net/html"
)

// parseDoc parses raw HTML into a document node tree, tolerating the
// malformed markup Bandcamp occasionally serves.
func parseDoc(page string) (*html.Node, error) {
	return html.Parse(strings.NewReader(page))
}

// attrJSON finds the first element carrying the named attribute (for
// example "data-blob" or "data-tralbum"), unescapes its value, and
// unmarshals it into out.
//
// x/net/html decodes HTML entities in attribute values during
// tokenization, so the &quot;-escaped JSON embedded by Bandcamp arrives
// already unescaped.
func attrJSON(doc *html.Node, attr string, out any) error {
	val, ok := findAttr(doc, attr)
	if !ok {
		return fmt.Errorf("no %s attribute found", attr)
	}
	return json.Unmarshal([]byte(val), out)
}

// findAttr walks the document (depth-first, document order) looking for
// the first element with the named attribute.
func findAttr(n *html.Node, attr string) (string, bool) {
	for c := n; c != nil; c = nextNode(c) {
		if c.Type == html.ElementNode {
			if v, ok := attrValue(c.Attr, attr); ok {
				return v, true
			}
		}
	}
	return "", false
}

// nextNode is the pre-order successor in a depth-first walk.
func nextNode(n *html.Node) *html.Node {
	if n.FirstChild != nil {
		return n.FirstChild
	}
	for s := n.NextSibling; s != nil; s = s.NextSibling {
		return s
	}
	for p := n.Parent; p != nil; p = p.Parent {
		if s := p.NextSibling; s != nil {
			return s
		}
	}
	return nil
}

// attrValue returns the value of one attribute of a node.
func attrValue(attrs []html.Attribute, key string) (string, bool) {
	for _, a := range attrs {
		if a.Key == key || a.Namespace+":"+a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

// elem finds the first descendant element with the given tag name
// (for example "a", "img", "p"), searching below n (not n itself).
func elem(n *html.Node, name string) *html.Node {
	for c := nextNode(n); c != nil; c = nextNode(c) {
		if c.Type == html.ElementNode && c.Data == name {
			return c
		}
	}
	return nil
}

// text collects the text content of a node, trimmed.
func text(n *html.Node) string {
	var buf bytes.Buffer
	var walk func(*html.Node)
	walk = func(m *html.Node) {
		if m.Type == html.TextNode {
			buf.WriteString(m.Data)
		}
		for c := m.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(buf.String())
}
