package main

import "testing"

func TestConvertMarkdownTablesToWhatsApp(t *testing.T) {
	input := "Before\n\n| File | Status | Notes |\n| --- | --- | --- |\n| main.go | changed | table formatting |\n| go.mod | unchanged | ok |\n\nAfter"
	want := "Before\n\n*main.go*\n• Status: changed\n• Notes: table formatting\n*go.mod*\n• Status: unchanged\n• Notes: ok\n\nAfter"

	got := convertMarkdownTablesToWhatsApp(input)
	if got != want {
		t.Fatalf("unexpected formatted table:\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestConvertMarkdownTablesLeavesPlainText(t *testing.T) {
	input := "No table here | just text"
	got := convertMarkdownTablesToWhatsApp(input)
	if got != input {
		t.Fatalf("plain text changed: %q", got)
	}
}
