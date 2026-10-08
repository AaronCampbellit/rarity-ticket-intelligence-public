package sales

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRenderProposalPDFProducesMultiPageDocument(t *testing.T) {
	lines := make([]ProposalLine, 45)
	for index := range lines {
		lines[index] = ProposalLine{
			Description: "Managed service line", Quantity: 1,
			UnitPrice: Money{Minor: 12500, Currency: "USD"},
		}
	}
	document, err := renderProposalPDF(ProposalVersion{
		Version: 3, Currency: "USD", Lines: lines,
		Subtotal: Money{Minor: 562500, Currency: "USD"},
		TaxTotal: Money{Minor: 45000, Currency: "USD"},
		Total:    Money{Minor: 607500, Currency: "USD"},
		IssuedAt: time.Date(2026, time.July, 30, 19, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("renderProposalPDF() error = %v", err)
	}
	for _, required := range [][]byte{
		[]byte("%PDF-1.7"), []byte("/Count 2"), []byte("Page 1 of 2"),
		[]byte("Page 2 of 2"), []byte("xref"), []byte("%%EOF"),
	} {
		if !bytes.Contains(document, required) {
			t.Fatalf("PDF missing %q", required)
		}
	}
	if output := os.Getenv("RTI_PDF_PREVIEW"); output != "" {
		if err := os.WriteFile(output, document, 0o600); err != nil {
			t.Fatalf("write PDF preview: %v", err)
		}
	}
}

func TestWrapPDFTextPreservesLongDescription(t *testing.T) {
	lines := wrapPDFText("A long proposal description that must remain visible in the immutable snapshot", 24)
	if len(lines) < 2 || strings.Join(lines, " ") !=
		"A long proposal description that must remain visible in the immutable snapshot" {
		t.Fatalf("unexpected wrapped description: %#v", lines)
	}
}
