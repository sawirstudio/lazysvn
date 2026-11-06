package main

import (
	"encoding/xml"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
)

// parseStatusXMLReference is the previous encoding/xml implementation, kept as
// an oracle: the one-pass scanner must agree with it on every document.
func parseStatusXMLReference(data []byte) ([]statusEntry, error) {
	type xmlEntry struct {
		Path     string `xml:"path,attr"`
		WCStatus struct {
			Item           string `xml:"item,attr"`
			Props          string `xml:"props,attr"`
			TreeConflicted string `xml:"tree-conflicted,attr"`
		} `xml:"wc-status"`
	}
	type xmlGroup struct {
		Name    string     `xml:"name,attr"`
		Entries []xmlEntry `xml:"entry"`
	}
	var doc struct {
		Targets     []xmlGroup `xml:"target"`
		Changelists []xmlGroup `xml:"changelist"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	var entries []statusEntry
	add := func(e xmlEntry, cl string) {
		ws := e.WCStatus
		if r, ok := makeStatusEntry(e.Path, cl, []byte(ws.Item), []byte(ws.Props), []byte(ws.TreeConflicted)); ok {
			entries = append(entries, r)
		}
	}
	for _, t := range doc.Targets {
		for _, e := range t.Entries {
			add(e, "")
		}
	}
	for _, c := range doc.Changelists {
		for _, e := range c.Entries {
			add(e, c.Name)
		}
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

func xmlAttr(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

var fuzzItems = []string{"added", "conflicted", "deleted", "external", "ignored", "incomplete", "merged", "missing", "modified", "none", "normal", "obstructed", "replaced", "unversioned"}
var fuzzProps = []string{"none", "normal", "modified", "conflicted"}

// svnStatusDoc builds a document shaped like real svn output, including the
// nested elements the scanner has to step over.
func svnStatusDoc(paths []string, changelist string, seed int) string {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<status>\n<target\n   path=\".\">\n")
	for i, p := range paths {
		if i == len(paths)/2 && changelist != "" {
			fmt.Fprintf(&b, "</target>\n<changelist\n   name=\"%s\">\n", xmlAttr(changelist))
		}
		item := fuzzItems[(seed+i)%len(fuzzItems)]
		props := fuzzProps[(seed+i*7)%len(fuzzProps)]
		tc := ""
		if (seed+i)%5 == 0 {
			tc = "\n   tree-conflicted=\"true\""
		}
		fmt.Fprintf(&b, "<entry\n   path=\"%s\">\n<wc-status\n   item=\"%s\"\n   props=\"%s\"%s>\n<commit\n   revision=\"3\">\n<author>a&lt;b&gt; &amp; c</author>\n<date>2026-11-06T02:29:11.142070Z</date>\n</commit>\n<lock><token>opaquelocktoken:x</token><owner>o</owner><comment>&quot;hi&quot;</comment></lock>\n</wc-status>\n<repos-status item=\"modified\" props=\"none\"/>\n</entry>\n", xmlAttr(p), item, props, tc)
	}
	if changelist != "" && len(paths) > 0 {
		b.WriteString("</changelist>\n")
	} else {
		b.WriteString("</target>\n")
	}
	b.WriteString("<!-- trailing comment -->\n</status>\n")
	return b.String()
}

func TestStatusScannerMatchesReference(t *testing.T) {
	cases := [][]string{
		{"a.txt", "dir/b.go", "c"},
		{`we"ird & <name>.txt`, "tab\there", "ünïcødé/文件.go", "apos'path", "&amp;literal"},
		{"img@2x.png", "-dash.txt", "spaces in name.txt"},
	}
	for seed := 0; seed < 30; seed++ {
		for _, paths := range cases {
			for _, cl := range []string{"", "my cl & <co>"} {
				doc := []byte(svnStatusDoc(paths, cl, seed))
				got, err := parseStatusXML(doc)
				want, werr := parseStatusXMLReference(doc)
				if err != nil || werr != nil {
					t.Fatalf("seed %d: err=%v ref err=%v\n%s", seed, err, werr, doc)
				}
				if !sameEntries(got, want) {
					t.Fatalf("seed %d cl %q:\n got  %+v\n want %+v", seed, cl, got, want)
				}
			}
		}
	}
}

func TestStatusScannerRejectsGarbage(t *testing.T) {
	for _, in := range []string{"", "svn: E155007: not a working copy", "<status", "<status><entry path=\"x\"", "<status><entry path=x>"} {
		if _, err := parseStatusXML([]byte(in)); err == nil {
			t.Errorf("parseStatusXML(%q) succeeded", in)
		}
	}
}

func FuzzStatusScanner(f *testing.F) {
	f.Add("a.txt", "b/c.go", "cl", 0)
	f.Add(`x&y"<>`, "\u00e9", "", 3)
	f.Fuzz(func(t *testing.T, p1, p2, cl string, seed int) {
		if seed < 0 {
			seed = -seed
		}
		if !validXMLText(p1) || !validXMLText(p2) || !validXMLText(cl) || p1 == "" || p2 == "" {
			t.Skip()
		}
		doc := []byte(svnStatusDoc([]string{p1, p2}, cl, seed%1000))
		got, err := parseStatusXML(doc)
		want, werr := parseStatusXMLReference(doc)
		if werr != nil {
			t.Skip()
		}
		if err != nil || !sameEntries(got, want) {
			t.Fatalf("got %+v (%v), want %+v", got, err, want)
		}
		_, _ = parseStatusXML([]byte(p1 + p2 + cl)) // arbitrary bytes must not panic
	})
}

func validXMLText(s string) bool {
	for _, r := range s {
		if r == 0xFFFD || (r < 0x20 && r != '\t' && r != '\n' && r != '\r') {
			return false
		}
	}
	return true
}

func sameEntries(a, b []statusEntry) bool { return slices.Equal(a, b) }
