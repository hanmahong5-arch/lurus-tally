// Package intakevision asks the existing LLM gateway to read a page and then
// drops any amount the page text does not contain.
package intakevision

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/hanmahong5-arch/lurus-tally/internal/domain/intake"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
)

const prompt = `只根据图片中真实可见的文字输出一个 JSON 对象，不要解释。看不到的字段用空字符串。字段：invoice_no,invoice_code,invoice_type,seller_tax_no,seller_name,issue_date,amount_excl_tax,tax_amount,total_amount,direction,seen,lines。seen 必须是图上数字和文字的原文。direction 只能是 in 或 out。lines 是数组，元素含 name,qty,unit_price。金额不要推算。`

// Parser calls the chat gateway. A nil client means vision is off.
type Parser struct{ Client *llmclient.Client }

// Parse implements the inbox vision port. Errors leave the capture as a stored file.
func (p *Parser) Parse(ctx context.Context, contentType string, body []byte) (intake.Parsed, error) {
	if p == nil || p.Client == nil || len(body) == 0 {
		return intake.Parsed{}, nil
	}
	if contentType == "" {
		contentType = "image/jpeg"
	}
	data := "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(body)
	resp, err := p.Client.Chat(ctx, "", []llmclient.Message{{
		Role: "user",
		Content: []map[string]any{
			{"type": "text", "text": prompt},
			{"type": "image_url", "image_url": map[string]string{"url": data}},
		},
	}}, nil)
	if err != nil {
		return intake.Parsed{}, err
	}
	raw := ""
	if len(resp.Choices) > 0 {
		raw = contentString(resp.Choices[0].Message.Content)
	}
	return intake.ApplyVision(decodeVision(raw)), nil
}

func contentString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

type visionJSON struct {
	InvoiceNo     string              `json:"invoice_no"`
	InvoiceCode   string              `json:"invoice_code"`
	InvoiceType   string              `json:"invoice_type"`
	SellerTaxNo   string              `json:"seller_tax_no"`
	SellerName    string              `json:"seller_name"`
	IssueDate     string              `json:"issue_date"`
	AmountExclTax string              `json:"amount_excl_tax"`
	TaxAmount     string              `json:"tax_amount"`
	TotalAmount   string              `json:"total_amount"`
	Direction     string              `json:"direction"`
	Seen          string              `json:"seen"`
	Lines         []intake.ParsedLine `json:"lines"`
}

func decodeVision(raw string) intake.VisionRaw {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	var v visionJSON
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &v); err != nil {
		return intake.VisionRaw{}
	}
	return intake.VisionRaw{
		InvoiceNo: v.InvoiceNo, InvoiceCode: v.InvoiceCode, InvoiceType: v.InvoiceType,
		SellerTaxNo: v.SellerTaxNo, SellerName: v.SellerName, IssueDate: v.IssueDate,
		AmountExclTax: v.AmountExclTax, TaxAmount: v.TaxAmount, TotalAmount: v.TotalAmount,
		Direction: v.Direction, Seen: v.Seen, Lines: v.Lines,
	}
}
