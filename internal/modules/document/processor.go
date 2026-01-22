package document

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/convert-studio/backend/internal/shared/storage"
	"go.uber.org/zap"
)

// Processor handles Pandoc operations
type Processor struct {
	storage    *storage.Service
	pandocPath string
	logger     *zap.Logger
}

// ProcessOptions contains options for document processing
type ProcessOptions struct {
	InputPath  string
	OutputPath string
	FromFormat string
	ToFormat   string
	Options    ConversionOptions
}

// NewProcessor creates a new document processor
func NewProcessor(storage *storage.Service, pandocPath string, logger *zap.Logger) *Processor {
	if pandocPath == "" {
		pandocPath = "pandoc"
	}
	return &Processor{
		storage:    storage,
		pandocPath: pandocPath,
		logger:     logger,
	}
}

// Process executes document conversion
func (p *Processor) Process(ctx context.Context, opts ProcessOptions) error {
	args := p.buildPandocArgs(opts)

	p.logger.Info("Executing Pandoc",
		zap.String("input", opts.InputPath),
		zap.String("output", opts.OutputPath),
		zap.Strings("args", args),
	)

	cmd := exec.CommandContext(ctx, p.pandocPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		p.logger.Error("Pandoc execution failed",
			zap.Error(err),
			zap.String("output", string(output)),
		)
		return fmt.Errorf("pandoc failed: %w - %s", err, string(output))
	}

	return nil
}

func (p *Processor) buildPandocArgs(opts ProcessOptions) []string {
	args := []string{
		opts.InputPath,
		"-o", opts.OutputPath,
	}

	// Set input format if specified
	if opts.FromFormat != "" {
		args = append(args, "-f", opts.FromFormat)
	}

	// Set output format if specified
	if opts.ToFormat != "" {
		args = append(args, "-t", opts.ToFormat)
	}

	// Add metadata
	if opts.Options.Metadata != nil {
		if opts.Options.Metadata.Title != "" {
			args = append(args, "--metadata", fmt.Sprintf("title=%s", opts.Options.Metadata.Title))
		}
		if opts.Options.Metadata.Author != "" {
			args = append(args, "--metadata", fmt.Sprintf("author=%s", opts.Options.Metadata.Author))
		}
		if opts.Options.Metadata.Date != "" {
			args = append(args, "--metadata", fmt.Sprintf("date=%s", opts.Options.Metadata.Date))
		}
	}

	// Table of contents
	if opts.Options.TableOfContents != nil && opts.Options.TableOfContents.Enabled {
		args = append(args, "--toc")
		if opts.Options.TableOfContents.Depth > 0 {
			args = append(args, fmt.Sprintf("--toc-depth=%d", opts.Options.TableOfContents.Depth))
		}
	}

	// Styling
	if opts.Options.Styling != nil {
		if opts.Options.Styling.CSS != "" {
			args = append(args, "--css", opts.Options.Styling.CSS)
		}
		if opts.Options.Styling.Template != "" {
			args = append(args, "--template", opts.Options.Styling.Template)
		}
		if opts.Options.Styling.HighlightStyle != "" {
			args = append(args, "--highlight-style", opts.Options.Styling.HighlightStyle)
		}
	}

	// Page settings for PDF
	if opts.Options.PageSettings != nil && (opts.ToFormat == "pdf" || strings.HasSuffix(opts.OutputPath, ".pdf")) {
		if opts.Options.PageSettings.PaperSize != "" {
			args = append(args, "-V", fmt.Sprintf("papersize=%s", opts.Options.PageSettings.PaperSize))
		}
		if opts.Options.PageSettings.Margins.Top != "" {
			args = append(args, "-V", fmt.Sprintf("margin-top=%s", opts.Options.PageSettings.Margins.Top))
		}
		if opts.Options.PageSettings.Margins.Bottom != "" {
			args = append(args, "-V", fmt.Sprintf("margin-bottom=%s", opts.Options.PageSettings.Margins.Bottom))
		}
		if opts.Options.PageSettings.Margins.Left != "" {
			args = append(args, "-V", fmt.Sprintf("margin-left=%s", opts.Options.PageSettings.Margins.Left))
		}
		if opts.Options.PageSettings.Margins.Right != "" {
			args = append(args, "-V", fmt.Sprintf("margin-right=%s", opts.Options.PageSettings.Margins.Right))
		}
	}

	// Bibliography
	if opts.Options.Bibliography != nil {
		if opts.Options.Bibliography.File != "" {
			args = append(args, "--citeproc", "--bibliography", opts.Options.Bibliography.File)
		}
		if opts.Options.Bibliography.Style != "" {
			args = append(args, "--csl", opts.Options.Bibliography.Style+".csl")
		}
	}

	// Advanced options
	if opts.Options.Advanced != nil {
		if opts.Options.Advanced.Standalone {
			args = append(args, "--standalone")
		}
		if opts.Options.Advanced.SelfContained {
			args = append(args, "--self-contained")
		}
		if opts.Options.Advanced.NumberSections {
			args = append(args, "--number-sections")
		}
	}

	return args
}

// GetSupportedFormats returns formats supported by Pandoc
func (p *Processor) GetSupportedFormats(ctx context.Context) ([]string, []string, error) {
	// Get input formats
	inputCmd := exec.CommandContext(ctx, p.pandocPath, "--list-input-formats")
	inputOutput, err := inputCmd.Output()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get input formats: %w", err)
	}

	// Get output formats
	outputCmd := exec.CommandContext(ctx, p.pandocPath, "--list-output-formats")
	outputOutput, err := outputCmd.Output()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get output formats: %w", err)
	}

	inputFormats := strings.Split(strings.TrimSpace(string(inputOutput)), "\n")
	outputFormats := strings.Split(strings.TrimSpace(string(outputOutput)), "\n")

	return inputFormats, outputFormats, nil
}
