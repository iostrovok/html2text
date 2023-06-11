package html2text

import (
	"bytes"
	"io"
	"regexp"
	"strings"
	"unicode"

	"github.com/olekukonko/tablewriter"
	"github.com/pkg/errors"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/iostrovok/html2text/bom"
)

type Handler func(string) (string, error)

type WHandler struct {
	Handler Handler
	Define  bool
}

type Html2Text struct {
	handlers map[atom.Atom]*WHandler
}

var EmptyHandler = func(a string) (string, error) {
	return a, nil
}

var allAtoms = []atom.Atom{
	atom.A, atom.B, atom.Blockquote, atom.Br, atom.Div, atom.H1, atom.H1, atom.H2, atom.H3, atom.Head,
	atom.Img, atom.Li, atom.P, atom.Pre, atom.Script, atom.Strong, atom.Style, atom.Table,
	atom.Td, atom.Tfoot, atom.Th, atom.Tr, atom.Ul,
}

func New() *Html2Text {
	handlers := map[atom.Atom]*WHandler{}
	for i := range allAtoms {
		handlers[allAtoms[i]] = &WHandler{EmptyHandler, false}
	}

	return &Html2Text{handlers: handlers}
}

func (h *Html2Text) SetHandler(key atom.Atom, handler Handler) *Html2Text {
	h.handlers[key] = &WHandler{handler, true}
	return h
}

func (h *Html2Text) SetHandlers(handlers map[atom.Atom]Handler) *Html2Text {
	for key, handler := range handlers {
		h.handlers[key] = &WHandler{handler, true}
	}

	return h
}

// Options provide toggles and overrides to control specific rendering behaviors.
type Options struct {
	PrettyTables        bool                 // Turns on pretty ASCII rendering for table elements.
	PrettyTablesOptions *PrettyTablesOptions // Configures pretty ASCII rendering for table elements.
	OmitLinks           bool                 // Turns on omitting links
	TextOnly            bool                 // Returns only plain text
}

// PrettyTablesOptions overrides tablewriter behaviors
type PrettyTablesOptions struct {
	AutoFormatHeader     bool
	AutoWrapText         bool
	ReflowDuringAutoWrap bool
	ColWidth             int
	ColumnSeparator      string
	RowSeparator         string
	CenterSeparator      string
	HeaderAlignment      int
	FooterAlignment      int
	Alignment            int
	ColumnAlignment      []int
	NewLine              string
	HeaderLine           bool
	RowLine              bool
	AutoMergeCells       bool
	Borders              tablewriter.Border
}

// NewPrettyTablesOptions creates PrettyTablesOptions with default settings
func NewPrettyTablesOptions() *PrettyTablesOptions {
	return &PrettyTablesOptions{
		AutoFormatHeader:     true,
		AutoWrapText:         true,
		ReflowDuringAutoWrap: true,
		ColWidth:             tablewriter.MAX_ROW_WIDTH,
		ColumnSeparator:      tablewriter.COLUMN,
		RowSeparator:         tablewriter.ROW,
		CenterSeparator:      tablewriter.CENTER,
		HeaderAlignment:      tablewriter.ALIGN_DEFAULT,
		FooterAlignment:      tablewriter.ALIGN_DEFAULT,
		Alignment:            tablewriter.ALIGN_DEFAULT,
		ColumnAlignment:      []int{},
		NewLine:              tablewriter.NEWLINE,
		HeaderLine:           true,
		RowLine:              false,
		AutoMergeCells:       false,
		Borders:              tablewriter.Border{Left: true, Right: true, Bottom: true, Top: true},
	}
}

// FromHTMLNode renders text output from a pre-parsed HTML document.
func (h *Html2Text) FromHTMLNode(doc *html.Node, o ...Options) (string, error) {
	var options Options
	if len(o) > 0 {
		options = o[0]
	}

	ctx := &textifyTraverseContext{
		buf:      bytes.Buffer{},
		options:  options,
		handlers: h.handlers,
	}

	if err := h.traverse(ctx, doc); err != nil {
		return "", err
	}

	text := strings.TrimSpace(newlineRe.ReplaceAllString(
		strings.Replace(ctx.buf.String(), "\n ", "\n", -1), "\n\n"),
	)
	return text, nil
}

// FromReader renders text output after parsing HTML for the specified
// io.Reader.
func (h *Html2Text) FromReader(reader io.Reader, options ...Options) (string, error) {
	newReader, err := bom.NewReaderWithoutBom(reader)
	if err != nil {
		return "", err
	}
	doc, err := html.Parse(newReader)
	if err != nil {
		return "", err
	}
	return h.FromHTMLNode(doc, options...)
}

// FromString parses HTML from the input string, then renders the text form.
func (h *Html2Text) FromString(input string, options ...Options) (string, error) {
	bs := bom.CleanBom([]byte(input))
	text, err := h.FromReader(bytes.NewReader(bs), options...)
	if err != nil {
		return "", err
	}
	return text, nil
}

var (
	spacingRe = regexp.MustCompile(`[ \r\n\t]+`)
	newlineRe = regexp.MustCompile(`\n\n+`)
)

// traverseTableCtx holds text-related context.
type textifyTraverseContext struct {
	buf bytes.Buffer

	handlers map[atom.Atom]*WHandler

	prefix          string
	tableCtx        tableTraverseContext
	options         Options
	endsWithSpace   bool
	justClosedDiv   bool
	blockquoteLevel int
	lineLength      int
	isPre           bool
}

// tableTraverseContext holds table ASCII-form related context.
type tableTraverseContext struct {
	header     []string
	body       [][]string
	footer     []string
	tmpRow     int
	isInFooter bool
}

func (tableCtx *tableTraverseContext) init() {
	tableCtx.body = [][]string{}
	tableCtx.header = []string{}
	tableCtx.footer = []string{}
	tableCtx.isInFooter = false
	tableCtx.tmpRow = 0
}

func (h *Html2Text) handleElement(ctx *textifyTraverseContext, node *html.Node) error {
	ctx.justClosedDiv = false

	switch node.DataAtom {
	case atom.Br:
		return h.emit(ctx, "\n")

	case atom.H1, atom.H2, atom.H3:
		subCtx := &textifyTraverseContext{
			handlers: ctx.handlers,
		}
		if err := h.traverseChildren(subCtx, node); err != nil {
			return err
		}

		str := subCtx.buf.String()
		if ctx.options.TextOnly {
			return h.emit(ctx, str+".\n\n")
		}
		dividerLen := 0
		for _, line := range strings.Split(str, "\n") {
			if lineLen := len([]rune(line)); lineLen-1 > dividerLen {
				dividerLen = lineLen - 1
			}
		}
		var divider string
		if node.DataAtom == atom.H1 {
			divider = strings.Repeat("*", dividerLen)
		} else {
			divider = strings.Repeat("-", dividerLen)
		}

		if node.DataAtom == atom.H3 {
			return h.emit(ctx, "\n\n"+str+"\n"+divider+"\n\n")
		}
		return h.emit(ctx, "\n\n"+divider+"\n"+str+"\n"+divider+"\n\n")

	case atom.Blockquote:
		ctx.blockquoteLevel++
		if !ctx.options.TextOnly {
			ctx.prefix = strings.Repeat(">", ctx.blockquoteLevel) + " "
		}
		if err := h.emit(ctx, "\n"); err != nil {
			return err
		}
		if ctx.blockquoteLevel == 1 {
			if err := h.emit(ctx, "\n"); err != nil {
				return err
			}
		}
		if err := h.traverseChildren(ctx, node); err != nil {
			return err
		}
		ctx.blockquoteLevel--
		if !ctx.options.TextOnly {
			ctx.prefix = strings.Repeat(">", ctx.blockquoteLevel)
		}
		if ctx.blockquoteLevel > 0 {
			ctx.prefix += " "
		}
		return h.emit(ctx, "\n\n")

	case atom.Div:
		if ctx.lineLength > 0 {
			if err := h.emit(ctx, "\n"); err != nil {
				return err
			}
		}
		if err := h.traverseChildren(ctx, node); err != nil {
			return err
		}
		var err error
		if !ctx.justClosedDiv {
			err = h.emit(ctx, "\n")
		}
		ctx.justClosedDiv = true
		return err

	case atom.Li:
		if !ctx.options.TextOnly {
			if err := h.emit(ctx, "* "); err != nil {
				return err
			}
		}

		if err := h.traverseChildren(ctx, node); err != nil {
			return err
		}

		return h.emit(ctx, "\n")

	case atom.B, atom.Strong:
		subCtx := &textifyTraverseContext{
			handlers: ctx.handlers,
		}
		subCtx.endsWithSpace = true
		if err := h.traverseChildren(subCtx, node); err != nil {
			return err
		}
		str := subCtx.buf.String()
		if ctx.options.TextOnly {
			return h.emit(ctx, str+".")
		}
		return h.emit(ctx, "*"+str+"*")

	case atom.A:
		linkText := ""
		// For simple link element content with single text node only, peek at the link text.
		if node.FirstChild != nil && node.FirstChild.NextSibling == nil && node.FirstChild.Type == html.TextNode {
			linkText = node.FirstChild.Data
		}

		// If image is the only child, take its alt text as the link text.
		if img := node.FirstChild; img != nil && node.LastChild == img && img.DataAtom == atom.Img {
			if altText := getAttrVal(img, "alt"); altText != "" {
				if err := h.emit(ctx, altText); err != nil {
					return err
				}
			}
		} else if err := h.traverseChildren(ctx, node); err != nil {
			return err
		}

		hrefLink := ""
		if href := getAttrVal(node, "href"); href != "" {
			href = ctx.normalizeHrefLink(href)

			// Don't print link href if it matches link element content or if the link is empty.
			if (href != "" && linkText != href) && !ctx.options.OmitLinks && !ctx.options.TextOnly {
				if h.handlers[atom.A].Define {
					hl, err := h.handlers[atom.A].Handler(href)
					if err != nil {
						return err
					}
					hrefLink = hl
				} else {
					hrefLink = "( " + href + " )"
				}
			}
		}

		return h.emit(ctx, hrefLink)

	case atom.P, atom.Ul:
		return h.paragraphHandler(ctx, node)

	case atom.Table, atom.Tfoot, atom.Th, atom.Tr, atom.Td:
		if ctx.options.PrettyTables {
			return h.handleTableElement(ctx, node)
		} else if node.DataAtom == atom.Table {
			return h.paragraphHandler(ctx, node)
		}
		return h.traverseChildren(ctx, node)

	case atom.Pre:
		ctx.isPre = true
		err := h.traverseChildren(ctx, node)
		ctx.isPre = false
		return err

	case atom.Style:
		// Ignore the subtree.
		return nil
	case atom.Head:
		// Ignore the subtree.
		return nil
	case atom.Script:
		// Ignore the subtree.
		return nil

	default:
		return h.traverseChildren(ctx, node)
	}
}

// paragraphHandler renders node children surrounded by double newlines.
func (h *Html2Text) paragraphHandler(ctx *textifyTraverseContext, node *html.Node) error {
	if err := h.emit(ctx, "\n\n"); err != nil {
		return err
	}
	if err := h.traverseChildren(ctx, node); err != nil {
		return err
	}
	return h.emit(ctx, "\n\n")
}

// handleTableElement is only to be invoked when options.PrettyTables is active.
func (h *Html2Text) handleTableElement(ctx *textifyTraverseContext, node *html.Node) error {
	if !ctx.options.PrettyTables {
		return errors.New("handleTableElement invoked when PrettyTables not active")
	}

	switch node.DataAtom {
	case atom.Table:
		if err := h.emit(ctx, "\n\n"); err != nil {
			return err
		}

		// Re-intialize all table context.
		ctx.tableCtx.init()

		// Browse children, enriching context with table data.
		if err := h.traverseChildren(ctx, node); err != nil {
			return err
		}

		buf := &bytes.Buffer{}
		table := tablewriter.NewWriter(buf)
		if ctx.options.PrettyTablesOptions != nil {
			options := ctx.options.PrettyTablesOptions
			table.SetAutoFormatHeaders(options.AutoFormatHeader)
			table.SetAutoWrapText(options.AutoWrapText)
			table.SetReflowDuringAutoWrap(options.ReflowDuringAutoWrap)
			table.SetColWidth(options.ColWidth)
			table.SetColumnSeparator(options.ColumnSeparator)
			table.SetRowSeparator(options.RowSeparator)
			table.SetCenterSeparator(options.CenterSeparator)
			table.SetHeaderAlignment(options.HeaderAlignment)
			table.SetFooterAlignment(options.FooterAlignment)
			table.SetAlignment(options.Alignment)
			table.SetColumnAlignment(options.ColumnAlignment)
			table.SetNewLine(options.NewLine)
			table.SetHeaderLine(options.HeaderLine)
			table.SetRowLine(options.RowLine)
			table.SetAutoMergeCells(options.AutoMergeCells)
			table.SetBorders(options.Borders)
		}
		table.SetHeader(ctx.tableCtx.header)
		table.SetFooter(ctx.tableCtx.footer)
		table.AppendBulk(ctx.tableCtx.body)

		// Render the table using ASCII.
		table.Render()
		if err := h.emit(ctx, buf.String()); err != nil {
			return err
		}

		return h.emit(ctx, "\n\n")

	case atom.Tfoot:
		ctx.tableCtx.isInFooter = true
		if err := h.traverseChildren(ctx, node); err != nil {
			return err
		}
		ctx.tableCtx.isInFooter = false

	case atom.Tr:
		ctx.tableCtx.body = append(ctx.tableCtx.body, []string{})
		if err := h.traverseChildren(ctx, node); err != nil {
			return err
		}
		ctx.tableCtx.tmpRow++

	case atom.Th:
		res, err := h.renderEachChild(node, ctx.options)
		if err != nil {
			return err
		}

		ctx.tableCtx.header = append(ctx.tableCtx.header, res)

	case atom.Td:
		res, err := h.renderEachChild(node, ctx.options)
		if err != nil {
			return err
		}

		if ctx.tableCtx.isInFooter {
			ctx.tableCtx.footer = append(ctx.tableCtx.footer, res)
		} else {
			ctx.tableCtx.body[ctx.tableCtx.tmpRow] = append(ctx.tableCtx.body[ctx.tableCtx.tmpRow], res)
		}

	}
	return nil
}

func (h *Html2Text) traverse(ctx *textifyTraverseContext, node *html.Node) error {
	switch node.Type {
	default:
		return h.traverseChildren(ctx, node)

	case html.TextNode:
		var data string
		if ctx.isPre {
			data = node.Data
		} else {
			data = strings.TrimSpace(spacingRe.ReplaceAllString(node.Data, " "))
		}
		return h.emit(ctx, data)

	case html.ElementNode:
		return h.handleElement(ctx, node)
	}
}

func (h *Html2Text) traverseChildren(ctx *textifyTraverseContext, node *html.Node) error {
	for c := node.FirstChild; c != nil; c = c.NextSibling {
		if err := h.traverse(ctx, c); err != nil {
			return err
		}
	}

	return nil
}

func (h *Html2Text) emit(ctx *textifyTraverseContext, data string) error {
	if data == "" {
		return nil
	}
	var (
		lines = breakLongLines(ctx, data)
		err   error
	)
	for _, line := range lines {
		runes := []rune(line)
		startsWithSpace := unicode.IsSpace(runes[0])
		if !startsWithSpace && !ctx.endsWithSpace && !strings.HasPrefix(data, ".") {
			if err = ctx.buf.WriteByte(' '); err != nil {
				return err
			}
			ctx.lineLength++
		}
		ctx.endsWithSpace = unicode.IsSpace(runes[len(runes)-1])
		for _, c := range line {
			if _, err = ctx.buf.WriteString(string(c)); err != nil {
				return err
			}
			ctx.lineLength++
			if c == '\n' {
				ctx.lineLength = 0
				if ctx.prefix != "" {
					if _, err = ctx.buf.WriteString(ctx.prefix); err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}

const maxLineLen = 74

func breakLongLines(ctx *textifyTraverseContext, data string) []string {
	// Only break lines when in blockquotes.
	if ctx.blockquoteLevel == 0 {
		return []string{data}
	}
	var (
		ret      []string
		runes    = []rune(data)
		l        = len(runes)
		existing = ctx.lineLength
	)
	if existing >= maxLineLen {
		ret = append(ret, "\n")
		existing = 0
	}
	for l+existing > maxLineLen {
		i := maxLineLen - existing
		for i >= 0 && !unicode.IsSpace(runes[i]) {
			i--
		}
		if i == -1 {
			// No spaces, so go the other way.
			i = maxLineLen - existing
			for i < l && !unicode.IsSpace(runes[i]) {
				i++
			}
		}
		ret = append(ret, string(runes[:i])+"\n")
		for i < l && unicode.IsSpace(runes[i]) {
			i++
		}
		runes = runes[i:]
		l = len(runes)
		existing = 0
	}
	if len(runes) > 0 {
		ret = append(ret, string(runes))
	}
	return ret
}

func (ctx *textifyTraverseContext) normalizeHrefLink(link string) string {
	link = strings.TrimSpace(link)
	link = strings.TrimPrefix(link, "mailto:")
	return link
}

// renderEachChild visits each direct child of a node and collects the sequence of
// textual representations separated by a single newline.
func (h *Html2Text) renderEachChild(node *html.Node, options Options) (string, error) {
	buf := &bytes.Buffer{}
	for c := node.FirstChild; c != nil; c = c.NextSibling {
		s, err := h.FromHTMLNode(c, options)
		if err != nil {
			return "", err
		}
		if _, err = buf.WriteString(s); err != nil {
			return "", err
		}
		if c.NextSibling != nil {
			if err = buf.WriteByte('\n'); err != nil {
				return "", err
			}
		}
	}
	return buf.String(), nil
}

func getAttrVal(node *html.Node, attrName string) string {
	for _, attr := range node.Attr {
		if attr.Key == attrName {
			return attr.Val
		}
	}

	return ""
}
