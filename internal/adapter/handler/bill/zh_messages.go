package bill

import (
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
)

const (
	msgNeedLogin     = "请先登录"
	msgBadBillID     = "单据编号不对，请返回列表重新打开"
	msgBadBody       = "请求内容格式不对，请刷新页面后重试"
	msgNoItems       = "请至少添加一行商品"
	msgBadBill       = "单据内容不符合要求，请检查后重试"
	msgApproveBusy   = "这张单据正在被审核，请稍后再试"
	msgPurchaseGone  = "找不到这张采购单"
	msgSaleGone      = "找不到这张销售单"
	msgShortOfStock  = "库存不够，请核对数量后重试"
	msgNotDraftApprv = "只有草稿单据才能审核，请刷新页面查看最新状态"
)

// bindFieldNames names request fields the way a shop owner calls them.
var bindFieldNames = map[string]string{
	"Remark":        "备注",
	"PaymentMethod": "付款方式",
	"Currency":      "币种",
	"ExchangeRate":  "汇率",
}

// bindErrMsg turns a JSON bind / validator failure into plain Chinese, never echoing the
// library's English text.
func bindErrMsg(err error) string {
	var verrs validator.ValidationErrors
	if errors.As(err, &verrs) && len(verrs) > 0 {
		fe := verrs[0]
		name := bindFieldNames[fe.Field()]
		if name == "" {
			name = "有一项内容"
		}
		switch fe.Tag() {
		case "max":
			return name + "最多 " + fe.Param() + " 个字，请删减后重试"
		case "required":
			return "请填写" + name
		}
		return name + "不符合要求，请检查后重试"
	}
	return msgBadBody
}

// fieldLabels maps a request field (without any items[N]. prefix) to its Chinese name.
var fieldLabels = map[string]string{
	"partner_id":    "往来单位",
	"warehouse_id":  "仓库",
	"bill_date":     "单据日期",
	"shipping_fee":  "运费",
	"tax_amount":    "税额",
	"currency":      "币种",
	"exchange_rate": "汇率",
	"product_id":    "商品",
	"qty":           "数量",
	"unit_price":    "单价",
	"unit_id":       "单位",
	"warehouse":     "仓库",
}

var itemFieldRe = regexp.MustCompile(`^items\[(\d+)\]\.(\w+)$`)

// fieldErrMsg renders a fieldError ("items[0].qty: must be ...") as a Chinese sentence.
func fieldErrMsg(fe *fieldError) string {
	label := fieldLabels[fe.field]
	prefix := ""
	if m := itemFieldRe.FindStringSubmatch(fe.field); m != nil {
		n, _ := strconv.Atoi(m[1])
		prefix = "第 " + strconv.Itoa(n+1) + " 行的"
		label = fieldLabels[m[2]]
	}
	if label == "" {
		label = "有一项内容"
	}
	name := prefix + label
	switch {
	case strings.Contains(fe.msg, "valid UUID"):
		return name + "选择有误，请刷新页面后重试"
	case strings.Contains(fe.msg, "RFC3339"):
		return name + "格式不对，请重新选择"
	case strings.Contains(fe.msg, "positive decimal"):
		return name + "要填大于 0 的数字"
	case strings.Contains(fe.msg, "non-negative"):
		return name + "要填不小于 0 的数字"
	case strings.Contains(fe.msg, "valid decimal"):
		return name + "要填数字"
	case strings.Contains(fe.msg, "3-letter"):
		return name + "不正确，请重新选择"
	case strings.Contains(fe.msg, "is required"):
		return "选了外币，请填写" + label
	case strings.Contains(fe.msg, "at most"):
		return name + "太大或小数位太多，请检查后重试"
	}
	return name + "不符合要求，请检查后重试"
}

// validationMsg renders an error from request building or a use case's ErrValidation in
// plain Chinese. Anything it cannot recognise falls back to a generic sentence, so English
// sentinel text never reaches the page.
func validationMsg(err error) string {
	var fe *fieldError
	if errors.As(err, &fe) {
		return fieldErrMsg(fe)
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "at least one item"):
		return msgNoItems
	case strings.Contains(s, "item qty must be positive"):
		return "数量要填大于 0 的数字"
	case strings.Contains(s, "item unit_price"):
		return "单价要填不小于 0 的数字"
	case strings.Contains(s, "discount_amount exceeds"):
		return "优惠金额不能超过商品合计"
	case strings.Contains(s, "discount_amount must"):
		return "优惠金额要填不小于 0 的数字"
	case strings.Contains(s, "paid_amount has more than"):
		return "实收金额最多保留两位小数"
	case strings.Contains(s, "paid_amount must"):
		return "付款金额要填不小于 0 的数字"
	case strings.Contains(s, "refund amount"):
		return "退款金额要填不小于 0 的数字"
	case strings.Contains(s, "exchange_rate"):
		return "汇率不正确，请检查后重试"
	case strings.Contains(s, "client_ref is longer"):
		return "单据编号参考过长，请刷新页面后重试"
	case strings.Contains(s, "unsupported source"):
		return "单据来源不正确，请刷新页面后重试"
	case strings.Contains(s, "occurred_at is in the future"):
		return "收银时间晚于当前时间，请校准设备时间后重试"
	case strings.Contains(s, "occurred_at is older"):
		return "收银时间距今超过 30 天，无法补录，请联系管理员处理"
	case strings.Contains(s, "product ") && strings.Contains(s, "does not exist"):
		return "有商品已不存在，请刷新页面后重试"
	case strings.Contains(s, "warehouse ") && strings.Contains(s, "does not exist"):
		return "所选仓库已不存在，请重新选择"
	}
	// Domain texts already written in Chinese (e.g. "往来单位不存在") pass through minus the prefix.
	if i := strings.LastIndex(s, ": "); i >= 0 && !asciiWordRe.MatchString(s[i+2:]) {
		return s[i+2:]
	}
	if !asciiWordRe.MatchString(s) {
		return s
	}
	return msgBadBill
}

var asciiWordRe = regexp.MustCompile(`[A-Za-z]{2,}`)

// zeroPriceMsg tells the approver which lines (and products, when known) still have
// no purchase price.
func zeroPriceMsg(lines []appbill.ZeroPriceLine, names map[uuid.UUID]string) string {
	parts := make([]string, 0, len(lines))
	for _, l := range lines {
		part := "第 " + strconv.Itoa(l.LineNo) + " 行"
		if n := strings.TrimSpace(names[l.ProductID]); n != "" {
			part += "「" + n + "」"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "、") + "的进货价还是 0，请先补上价格；如果是赠品，请确认后按 0 入账"
}
