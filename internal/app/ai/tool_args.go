package ai

import (
	"encoding/json"

	"github.com/invopop/jsonschema"
)

// Each tool's arguments are one named struct: the tool parses the model's
// call into it, and its JSON schema — the parameters the model is shown — is
// generated from it. They were two hand-kept copies (a map literal here, an
// anonymous struct in the tool), free to drift apart.
// testdata/tool_schemas.golden.json pins the generated schemas.

type noArgs struct{}

type searchProductsArgs struct {
	Query string `json:"query" jsonschema:"required" jsonschema_description:"Search string"`
}

type listLowStockArgs struct {
	ThresholdDays *int `json:"threshold_days" jsonschema_description:"Days of supply threshold (default 7)"`
}

type listDeadStockArgs struct {
	Days *int `json:"days" jsonschema_description:"Inactivity threshold in days (default 90)"`
}

type recentSalesTopArgs struct {
	Metric string `json:"metric" jsonschema:"required,enum=revenue,enum=margin,enum=qty"`
	Days   *int   `json:"days" jsonschema_description:"Lookback days (default 7)"`
	Limit  *int   `json:"limit" jsonschema_description:"Number of results (default 10)"`
}

type grossMarginSummaryArgs struct {
	Days *int `json:"days" jsonschema_description:"Lookback days (default 30)"`
}

type customerRecentPurchasesArgs struct {
	Customer string `json:"customer" jsonschema:"required" jsonschema_description:"Customer name as the user said it"`
	Limit    *int   `json:"limit" jsonschema_description:"Number of latest bills (default 5, max 20)"`
}

type queryExchangeRateArgs struct {
	From string `json:"from" jsonschema:"required" jsonschema_description:"Source currency code (e.g. USD)"`
	To   string `json:"to" jsonschema_description:"Target currency code (default CNY)"`
}

type proposePriceChangeArgs struct {
	Filter string `json:"filter" jsonschema:"required" jsonschema_description:"Search filter to select products (e.g. brand name, category)"`
	Action string `json:"action" jsonschema:"required" jsonschema_description:"Price action: '+5%', '-10%', '=199.00'"`
}

type purchaseDraftItem struct {
	ProductName string  `json:"product_name"`
	Qty         float64 `json:"qty"`
}

type proposeCreatePurchaseDraftArgs struct {
	Items []purchaseDraftItem `json:"items" jsonschema:"required"`
}

type proposeBulkStockAdjustArgs struct {
	Filter string  `json:"filter" jsonschema:"required" jsonschema_description:"Search filter to select products"`
	Delta  float64 `json:"delta" jsonschema:"required" jsonschema_description:"Quantity delta to apply (positive = in, negative = out)"`
}

type rememberCustomerFactArgs struct {
	Customer  string `json:"customer" jsonschema:"required" jsonschema_description:"Customer as the user named them, including address forms like 老张/王老板/李总"`
	Attribute string `json:"attribute" jsonschema:"required" jsonschema_description:"Which attribute of the customer the statement is about"`
	Value     string `json:"value" jsonschema:"required" jsonschema_description:"The attribute's value now, short (e.g. 微信, 城南路18号, 每周四, 他儿子)"`
	Quote     string `json:"quote" jsonschema:"required" jsonschema_description:"The part of the user's message that states it, verbatim"`
}

// JSONSchemaExtend lists the attribute slots, which come from the customer
// ontology rather than a tag.
func (rememberCustomerFactArgs) JSONSchemaExtend(s *jsonschema.Schema) {
	if p, ok := s.Properties.Get("attribute"); ok {
		for _, id := range customerSlotIDs() {
			p.Enum = append(p.Enum, id)
		}
	}
}

type recallAsOfArgs struct {
	Customer string `json:"customer" jsonschema:"required" jsonschema_description:"Customer as the user named them, including address forms like 老张/王老板/李总"`
	AsOf     string `json:"as_of" jsonschema:"required" jsonschema_description:"The moment the user asks about: YYYY-MM-DD, YYYY-MM, or just the month (\"03\") when the user names no year — the most recent such month is used. A period means its end. Convert relative times (上个月, 上周三, 前天) to a date from today's date; never pass the words themselves."`
	Topic    string `json:"topic" jsonschema_description:"What the user asks about, in their words (e.g. 付款方式, 送货地址); optional"`
}

// toolParams is the parameters schema of a tool whose arguments are args:
// a bare object schema (no $schema/$id/$defs), fields required only when
// tagged so, and no additionalProperties keyword.
func toolParams(args any) json.RawMessage {
	r := jsonschema.Reflector{
		Anonymous:                  true,
		DoNotReference:             true,
		ExpandedStruct:             true,
		AllowAdditionalProperties:  true,
		RequiredFromJSONSchemaTags: true,
	}
	s := r.Reflect(args)
	s.Version = ""
	b, err := json.Marshal(s)
	if err == nil && s.Properties == nil {
		// A tool without arguments still sends `"properties": {}`, as the
		// function-calling APIs expect an object schema to list them.
		var m map[string]any
		if err = json.Unmarshal(b, &m); err == nil {
			m["properties"] = map[string]any{}
			b, err = json.Marshal(m)
		}
	}
	if err != nil {
		panic("tool schema: " + err.Error()) // static types; cannot fail at run time
	}
	return b
}
