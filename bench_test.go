package main

import (
	"fmt"
	"strings"
	"testing"
)

// Benchmarks for the per-frame and per-refresh hot paths:
//
//	go test -run '^$' -bench . -benchmem

func bigDiffDoc(n int) *diffDoc {
	raw := []string{"Index: big.go", "--- big.go\t(revision 1)", "+++ big.go\t(working copy)", fmt.Sprintf("@@ -1,%d +1,%d @@", n, n)}
	for i := 0; i < n; i++ {
		switch i % 3 {
		case 0:
			raw = append(raw, " context line "+strings.Repeat("x", 60))
		case 1:
			raw = append(raw, "-old line "+strings.Repeat("y", 60))
		default:
			raw = append(raw, "+new line "+strings.Repeat("z", 60))
		}
	}
	return newDiffDoc(raw)
}

func manyEntries(n int) []statusEntry {
	es := make([]statusEntry, n)
	for i := range es {
		es[i] = statusEntry{Code: 'M', PropCode: ' ', Path: fmt.Sprintf("src/pkg%04d/file%05d.go", i%500, i)}
	}
	return es
}

func benchModel(entries ...statusEntry) model {
	m := initialModel()
	m.width, m.height = 160, 50
	return m.onStatus(statusMsg{seq: m.statusSeq, entries: entries})
}

func BenchmarkViewDiff10k(b *testing.B) {
	m := benchModel(manyEntries(3)...)
	m.preview = preview{kind: previewDiff, title: "Diff: big.go", path: "big.go", diff: bigDiffDoc(10000)}
	b.ReportAllocs()
	for b.Loop() {
		_ = m.View()
	}
}

func BenchmarkViewText100k(b *testing.B) {
	m := benchModel(manyEntries(3)...)
	m.preview = preview{kind: previewAction, title: "Update"}.withText(strings.Repeat("U    some/updated/file.go\n", 100000))
	b.ReportAllocs()
	for b.Loop() {
		_ = m.View()
	}
}

func BenchmarkViewHelp(b *testing.B) {
	m := benchModel(manyEntries(3)...)
	b.ReportAllocs()
	for b.Loop() {
		_ = m.View()
	}
}

func BenchmarkView20kEntries(b *testing.B) {
	m := benchModel(manyEntries(20000)...)
	for i := 0; i < len(m.entries); i += 3 {
		m.marked[m.entries[i].Path] = struct{}{}
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = m.View()
	}
}

func BenchmarkNewDiffDoc10k(b *testing.B) {
	raw := []string{"@@ -1,10000 +1,10000 @@"}
	for i := 0; i < 10000; i++ {
		raw = append(raw, []string{" ctx", "-old", "+new"}[i%3]+strings.Repeat("x", 60))
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = newDiffDoc(raw)
	}
}

func statusXML20k() []byte {
	paths := make([]string, 20000)
	for i := range paths {
		paths[i] = fmt.Sprintf("src/pkg%04d/file%05d.go", i%500, i)
	}
	return []byte(svnStatusDoc(paths, "", 0))
}

func BenchmarkParseStatusXML20k(b *testing.B) {
	data := statusXML20k()
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := parseStatusXML(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseStatusXML20kReference(b *testing.B) {
	data := statusXML20k()
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := parseStatusXMLReference(data); err != nil {
			b.Fatal(err)
		}
	}
}
