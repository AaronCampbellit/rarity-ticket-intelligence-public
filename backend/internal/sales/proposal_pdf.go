package sales

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

const proposalPDFLinesPerPage = 38

func renderProposalPDF(version ProposalVersion) ([]byte, error) {
	lines := []string{
		"PROPOSAL",
		"Proposal version: " + strconv.FormatInt(version.Version, 10),
		"Issued: " + version.IssuedAt.UTC().Format("2006-01-02 15:04 UTC"),
		"",
		"DESCRIPTION | QTY | UNIT PRICE | LINE TOTAL",
	}
	for _, line := range version.Lines {
		total := line.Quantity*line.UnitPrice.Minor - line.Discount.Minor + line.Tax.Minor
		descriptionLines := wrapPDFText(line.Description, 42)
		lines = append(lines, fmt.Sprintf("%s | %d | %s | %s",
			descriptionLines[0], line.Quantity, formatPDFMoney(line.UnitPrice),
			formatPDFMoney(Money{Minor: total, Currency: version.Currency})))
		for _, continuation := range descriptionLines[1:] {
			lines = append(lines, "  "+continuation)
		}
	}
	lines = append(lines, "", "Subtotal: "+formatPDFMoney(version.Subtotal),
		"Tax: "+formatPDFMoney(version.TaxTotal),
		"Total: "+formatPDFMoney(version.Total))
	pageCount := (len(lines) + proposalPDFLinesPerPage - 1) / proposalPDFLinesPerPage
	objects := make([][]byte, 4+pageCount*2)
	objects[1] = []byte("<< /Type /Catalog /Pages 2 0 R >>")
	kids := make([]string, 0, pageCount)
	for page := 0; page < pageCount; page++ {
		pageObject, contentObject := 4+page*2, 5+page*2
		kids = append(kids, fmt.Sprintf("%d 0 R", pageObject))
		start, end := page*proposalPDFLinesPerPage, min((page+1)*proposalPDFLinesPerPage, len(lines))
		stream := proposalPDFStream(lines[start:end], page+1, pageCount)
		objects[pageObject] = []byte(fmt.Sprintf(
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>",
			contentObject))
		objects[contentObject] = []byte(fmt.Sprintf(
			"<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	}
	objects[2] = []byte(fmt.Sprintf("<< /Type /Pages /Count %d /Kids [%s] >>",
		pageCount, strings.Join(kids, " ")))
	objects[3] = []byte("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")

	var output bytes.Buffer
	output.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int, len(objects))
	for index := 1; index < len(objects); index++ {
		offsets[index] = output.Len()
		fmt.Fprintf(&output, "%d 0 obj\n", index)
		output.Write(objects[index])
		output.WriteString("\nendobj\n")
	}
	xref := output.Len()
	fmt.Fprintf(&output, "xref\n0 %d\n0000000000 65535 f \n", len(objects))
	for index := 1; index < len(objects); index++ {
		fmt.Fprintf(&output, "%010d 00000 n \n", offsets[index])
	}
	fmt.Fprintf(&output, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objects), xref)
	return output.Bytes(), nil
}

func wrapPDFText(value string, width int) []string {
	words := strings.Fields(value)
	if len(words) == 0 {
		return []string{""}
	}
	lines := []string{words[0]}
	for _, word := range words[1:] {
		last := len(lines) - 1
		if len(lines[last])+1+len(word) <= width {
			lines[last] += " " + word
		} else {
			lines = append(lines, word)
		}
	}
	return lines
}

func proposalPDFStream(lines []string, page, pageCount int) string {
	var stream strings.Builder
	stream.WriteString("BT\n/F1 10 Tf\n50 742 Td\n")
	if page > 1 {
		lines = append([]string{"PROPOSAL (continued)", ""}, lines...)
	}
	for index, line := range lines {
		if index > 0 {
			stream.WriteString("0 -17 Td\n")
		}
		fmt.Fprintf(&stream, "(%s) Tj\n", escapePDFText(line))
	}
	stream.WriteString("ET\nBT\n/F1 9 Tf\n500 30 Td\n")
	fmt.Fprintf(&stream, "(Page %d of %d) Tj\nET", page, pageCount)
	return stream.String()
}

func escapePDFText(value string) string {
	var result strings.Builder
	for _, character := range value {
		switch {
		case character == '\\' || character == '(' || character == ')':
			result.WriteByte('\\')
			result.WriteRune(character)
		case character >= 32 && character <= 126:
			result.WriteRune(character)
		default:
			result.WriteByte('?')
		}
	}
	return result.String()
}

func formatPDFMoney(money Money) string {
	sign, minor := "", money.Minor
	if minor < 0 {
		sign, minor = "-", -minor
	}
	return fmt.Sprintf("%s%s %d.%02d", sign, money.Currency, minor/100, minor%100)
}
