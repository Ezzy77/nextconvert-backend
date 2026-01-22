package document

import (
	"context"
	"fmt"

	"github.com/convert-studio/backend/internal/modules/jobs"
	"github.com/convert-studio/backend/internal/shared/storage"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Module handles document operations
type Module struct {
	storage   *storage.Service
	jobQueue  *jobs.QueueClient
	logger    *zap.Logger
	templates map[string]Template
}

// ConversionOptions contains document conversion options
type ConversionOptions struct {
	TargetFormat    string          `json:"targetFormat"`
	Metadata        *Metadata       `json:"metadata,omitempty"`
	TableOfContents *TOCOptions     `json:"tableOfContents,omitempty"`
	Styling         *StylingOptions `json:"styling,omitempty"`
	PageSettings    *PageSettings   `json:"pageSettings,omitempty"`
	Bibliography    *BibOptions     `json:"bibliography,omitempty"`
	Advanced        *AdvancedOptions `json:"advanced,omitempty"`
}

// Metadata contains document metadata
type Metadata struct {
	Title    string   `json:"title,omitempty"`
	Author   string   `json:"author,omitempty"`
	Date     string   `json:"date,omitempty"`
	Subject  string   `json:"subject,omitempty"`
	Keywords []string `json:"keywords,omitempty"`
}

// TOCOptions contains table of contents options
type TOCOptions struct {
	Enabled bool `json:"enabled"`
	Depth   int  `json:"depth"`
}

// StylingOptions contains document styling options
type StylingOptions struct {
	CSS            string `json:"css,omitempty"`
	Template       string `json:"template,omitempty"`
	HighlightStyle string `json:"highlightStyle,omitempty"`
	FontFamily     string `json:"fontFamily,omitempty"`
	FontSize       string `json:"fontSize,omitempty"`
}

// PageSettings contains page layout settings
type PageSettings struct {
	PaperSize   string  `json:"paperSize"`
	Orientation string  `json:"orientation"`
	Margins     Margins `json:"margins"`
}

// Margins contains margin settings
type Margins struct {
	Top    string `json:"top"`
	Bottom string `json:"bottom"`
	Left   string `json:"left"`
	Right  string `json:"right"`
}

// BibOptions contains bibliography options
type BibOptions struct {
	File  string `json:"file"`
	Style string `json:"style"`
}

// AdvancedOptions contains advanced conversion options
type AdvancedOptions struct {
	Standalone     bool `json:"standalone"`
	SelfContained  bool `json:"selfContained"`
	NumberSections bool `json:"numberSections"`
}

// Template represents a document template
type Template struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Format      string `json:"format"`
	Description string `json:"description"`
	Content     string `json:"content"`
	IsSystem    bool   `json:"isSystem"`
	UserID      string `json:"userId,omitempty"`
}

// ValidationResult contains conversion validation results
type ValidationResult struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// FormatSupport describes format conversion support
type FormatSupport struct {
	From   string   `json:"from"`
	To     []string `json:"to"`
}

// CitationStyle describes a citation style
type CitationStyle struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// NewModule creates a new document module
func NewModule(storage *storage.Service, jobQueue *jobs.QueueClient, logger *zap.Logger) *Module {
	m := &Module{
		storage:   storage,
		jobQueue:  jobQueue,
		logger:    logger,
		templates: make(map[string]Template),
	}

	m.initTemplates()
	return m
}

func (m *Module) initTemplates() {
	// Academic paper template
	m.templates["academic"] = Template{
		ID:          "academic",
		Name:        "Academic Paper",
		Format:      "pdf",
		Description: "Professional academic paper format with proper margins and citations",
		IsSystem:    true,
		Content:     `\\documentclass{article}\\usepackage[margin=1in]{geometry}`,
	}

	// Business report template
	m.templates["business-report"] = Template{
		ID:          "business-report",
		Name:        "Business Report",
		Format:      "pdf",
		Description: "Corporate report format with executive summary section",
		IsSystem:    true,
	}

	// Simple HTML template
	m.templates["simple-html"] = Template{
		ID:          "simple-html",
		Name:        "Simple HTML",
		Format:      "html",
		Description: "Clean, minimal HTML output",
		IsSystem:    true,
	}

	// Resume template
	m.templates["resume"] = Template{
		ID:          "resume",
		Name:        "Resume/CV",
		Format:      "pdf",
		Description: "Professional resume format",
		IsSystem:    true,
	}
}

// GetFormatMatrix returns the format conversion matrix
func (m *Module) GetFormatMatrix() []FormatSupport {
	return []FormatSupport{
		{From: "md", To: []string{"pdf", "docx", "html", "epub", "latex", "odt", "rtf"}},
		{From: "docx", To: []string{"pdf", "html", "epub", "md", "latex", "odt", "rtf"}},
		{From: "html", To: []string{"pdf", "docx", "epub", "md", "latex", "odt", "rtf"}},
		{From: "epub", To: []string{"pdf", "docx", "html", "md", "latex", "odt", "rtf"}},
		{From: "latex", To: []string{"pdf", "docx", "html", "epub", "md", "odt", "rtf"}},
		{From: "rst", To: []string{"pdf", "docx", "html", "epub", "md", "latex", "odt", "rtf"}},
		{From: "odt", To: []string{"pdf", "docx", "html", "epub", "md", "latex", "rtf"}},
		{From: "txt", To: []string{"pdf", "docx", "html", "epub", "md", "latex", "odt", "rtf"}},
	}
}

// GetTemplates returns templates, optionally filtered by format
func (m *Module) GetTemplates(format string) []Template {
	templates := make([]Template, 0)
	for _, t := range m.templates {
		if format == "" || t.Format == format {
			templates = append(templates, t)
		}
	}
	return templates
}

// GetTemplate returns a specific template
func (m *Module) GetTemplate(id string) (*Template, error) {
	t, ok := m.templates[id]
	if !ok {
		return nil, fmt.Errorf("template not found: %s", id)
	}
	return &t, nil
}

// CreateTemplate creates a new custom template
func (m *Module) CreateTemplate(t Template) (*Template, error) {
	t.ID = uuid.New().String()
	t.IsSystem = false
	m.templates[t.ID] = t
	return &t, nil
}

// ValidateConversion validates conversion options
func (m *Module) ValidateConversion(fromFormat, toFormat string, options ConversionOptions) ValidationResult {
	result := ValidationResult{Valid: true}

	// Check if conversion is supported
	supported := false
	matrix := m.GetFormatMatrix()
	for _, f := range matrix {
		if f.From == fromFormat {
			for _, to := range f.To {
				if to == toFormat {
					supported = true
					break
				}
			}
			break
		}
	}

	if !supported {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("Conversion from %s to %s is not supported", fromFormat, toFormat))
	}

	// Validate page settings for PDF output
	if toFormat == "pdf" && options.PageSettings != nil {
		validSizes := []string{"a4", "letter", "legal"}
		sizeValid := false
		for _, s := range validSizes {
			if options.PageSettings.PaperSize == s {
				sizeValid = true
				break
			}
		}
		if !sizeValid && options.PageSettings.PaperSize != "" {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Unknown paper size: %s, defaulting to A4", options.PageSettings.PaperSize))
		}
	}

	return result
}

// GetCitationStyles returns available citation styles
func (m *Module) GetCitationStyles() []CitationStyle {
	return []CitationStyle{
		{ID: "apa", Name: "APA (7th edition)", Description: "American Psychological Association"},
		{ID: "mla", Name: "MLA (9th edition)", Description: "Modern Language Association"},
		{ID: "chicago", Name: "Chicago (17th edition)", Description: "Chicago Manual of Style"},
		{ID: "harvard", Name: "Harvard", Description: "Harvard referencing style"},
		{ID: "ieee", Name: "IEEE", Description: "Institute of Electrical and Electronics Engineers"},
		{ID: "vancouver", Name: "Vancouver", Description: "Vancouver citation style for biomedical sciences"},
	}
}

// Convert initiates a document conversion job
func (m *Module) Convert(ctx context.Context, fileID string, options ConversionOptions) (string, error) {
	// TODO: Queue conversion job
	return "", nil
}
