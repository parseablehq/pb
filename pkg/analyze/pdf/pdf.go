package pdf

import (
	"fmt"
	"pb/pkg/common"
	"strings"
	"time"

	"github.com/jung-kurt/gofpdf"
)

// Updated createPDF function
func CreatePDF(summary, rootCause, mitigation, namespace, pod string) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	pdf.SetFont("Arial", "B", 16)

	// Title
	pdf.Cell(40, 10, "Postmortem Report")
	pdf.Ln(12)

	// Add sections
	addSection(pdf, "Summary:", summary)
	addSection(pdf, "Root Cause Analysis:", rootCause)
	addSection(pdf, "Mitigation Steps:", mitigation)

	// Generate filename with timestamp
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("postmortem-%s-%s-%s.pdf", sanitize(namespace), sanitize(pod), timestamp)

	// Save file
	err := pdf.OutputFileAndClose(filename)
	if err != nil {
		fmt.Println("Error generating PDF:", err)
	} else {
		fmt.Printf(common.Green+"Report saved as %s"+common.Reset+"\n", filename)
	}
}

// Helper function to sanitize filenames by replacing invalid characters
func sanitize(input string) string {
	return strings.ReplaceAll(input, "/", "_") // Replace slashes with underscores
}

func addSection(pdf *gofpdf.Fpdf, title, content string) {
	pdf.SetFont("Arial", "B", 14)
	pdf.Cell(0, 10, title)
	pdf.Ln(10)

	pdf.SetFont("Arial", "", 12)
	pdf.MultiCell(0, 10, content, "", "", false)
	pdf.Ln(10)
}
