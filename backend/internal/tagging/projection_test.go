package tagging

import "testing"

func TestCanonicalTagPairOrdersStableIdentity(t *testing.T) {
	left, right, ok := CanonicalTagPair("tag-z", "tag-a")
	if !ok || left != "tag-a" || right != "tag-z" {
		t.Fatalf("pair=(%q,%q) ok=%t", left, right, ok)
	}
	if _, _, ok := CanonicalTagPair("same", "same"); ok {
		t.Fatal("self pair must be rejected")
	}
}
