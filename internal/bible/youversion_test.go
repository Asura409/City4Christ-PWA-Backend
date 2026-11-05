package bible

import "testing"

func TestParseYouVersionHTMLAddsVerseBoundaries(t *testing.T) {
	verses := parseYouVersionHTML(`<p><span class="yv-v" v="7">The Lord is my strength.</span><span class="yv-v" v="8"><em>Trust in him.</em></span></p>`, "PSA.28.7-8")
	if len(verses) != 2 {
		t.Fatalf("got %d verses, want 2", len(verses))
	}
	if verses[0].Number != 7 || verses[0].Text != "The Lord is my strength." {
		t.Fatalf("unexpected first verse: %#v", verses[0])
	}
	if verses[1].Number != 8 || verses[1].Text != "Trust in him." {
		t.Fatalf("unexpected second verse: %#v", verses[1])
	}
}
