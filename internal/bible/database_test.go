package bible

import "testing"

func TestParseDatabaseReference(t *testing.T) {
	tests := []struct {
		name                                           string
		input                                          string
		startChapter, endChapter, startVerse, endVerse int
		hasVerse                                       bool
	}{
		{name: "whole chapter", input: "John 3", startChapter: 3, endChapter: 3},
		{name: "single verse", input: "John 3:16", startChapter: 3, endChapter: 3, startVerse: 16, endVerse: 16, hasVerse: true},
		{name: "same chapter range", input: "John 3:16-18", startChapter: 3, endChapter: 3, startVerse: 16, endVerse: 18, hasVerse: true},
		{name: "cross chapter range", input: "John 3:16-4:2", startChapter: 3, endChapter: 4, startVerse: 16, endVerse: 2, hasVerse: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := parseDatabaseReference(tt.input)
			if err != nil {
				t.Fatalf("parseDatabaseReference() error = %v", err)
			}
			if got.startChapter != tt.startChapter || got.endChapter != tt.endChapter || got.startVerse != tt.startVerse || got.endVerse != tt.endVerse || got.hasVerse != tt.hasVerse {
				t.Fatalf("parseDatabaseReference() = %#v", got)
			}
		})
	}
}

func TestParseDatabaseReferenceRejectsInvalidRanges(t *testing.T) {
	for _, input := range []string{"John 3:18-16", "John 4:2-3:16", "John 0", "John 3:0"} {
		if _, _, err := parseDatabaseReference(input); err == nil {
			t.Errorf("parseDatabaseReference(%q) accepted invalid range", input)
		}
	}
}
