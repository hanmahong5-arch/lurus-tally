package print

import (
	"context"
	"crypto/sha1" //nolint:gosec // 飞鹅开放平台规定的签名算法
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultFeieAPIURL is the vendor's open API endpoint.
const DefaultFeieAPIURL = "http://api.feieyun.cn/Api/Open/"

const feieTimeout = 8 * time.Second

// Config configures the Feie (飞鹅) cloud printer client.
// User is the vendor account, UKey the account's open-API key (both from the
// vendor console). APIURL and HTTPClient default when empty.
type Config struct {
	User       string
	UKey       string
	APIURL     string
	HTTPClient *http.Client
}

// FeieClient implements Printer on the Feie open API.
type FeieClient struct {
	user, ukey, apiURL string
	client             *http.Client
	now                func() time.Time
}

var _ Printer = (*FeieClient)(nil)

// New returns (nil, nil) when User or UKey is empty, so the composition root can
// wire it unconditionally and nil-check before use, like the other optional
// integrations. A nil result is a nil interface only if the caller keeps the
// concrete type; use NewPrinter for that.
func New(cfg Config) (*FeieClient, error) {
	if cfg.User == "" || cfg.UKey == "" {
		return nil, nil
	}
	c := &FeieClient{user: cfg.User, ukey: cfg.UKey, apiURL: cfg.APIURL, client: cfg.HTTPClient, now: time.Now}
	if c.apiURL == "" {
		c.apiURL = DefaultFeieAPIURL
	}
	if c.client == nil {
		c.client = &http.Client{Timeout: feieTimeout}
	}
	return c, nil
}

// NewPrinter is New returning the interface: a nil Printer when not configured.
func NewPrinter(cfg Config) (Printer, error) {
	c, err := New(cfg)
	if err != nil || c == nil {
		return nil, err
	}
	return c, nil
}

// sign is sha1(USER + UKEY + STIME) in lower-case hex, as the vendor specifies.
func sign(user, ukey, stime string) string {
	sum := sha1.Sum([]byte(user + ukey + stime)) //nolint:gosec
	return hex.EncodeToString(sum[:])
}

type feieReply struct {
	Msg  string          `json:"msg"`
	Ret  int             `json:"ret"`
	Data json.RawMessage `json:"data"`
}

// call posts one API form and returns the data field of a ret==0 reply.
func (c *FeieClient) call(ctx context.Context, apiname string, extra url.Values) (json.RawMessage, error) {
	stime := strconv.FormatInt(c.now().Unix(), 10)
	form := url.Values{}
	for k, v := range extra {
		form[k] = v
	}
	form.Set("user", c.user)
	form.Set("stime", stime)
	form.Set("sig", sign(c.user, c.ukey, stime))
	form.Set("apiname", apiname)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("print/feie: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("print/feie: %s: %w", apiname, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("print/feie: %s: read body: %w", apiname, err)
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("print/feie: %s: http %d", apiname, resp.StatusCode)
	}
	var r feieReply
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("print/feie: %s: decode reply: %w", apiname, err)
	}
	if r.Ret != 0 {
		return nil, fmt.Errorf("print/feie: %s: ret=%d msg=%s", apiname, r.Ret, r.Msg)
	}
	return r.Data, nil
}

// Print implements Printer via Open_printMsg. times is clamped to 1..9 copies.
func (c *FeieClient) Print(ctx context.Context, sn string, content string, times int) (string, error) {
	if times < 1 {
		times = 1
	}
	if times > 9 {
		times = 9
	}
	data, err := c.call(ctx, "Open_printMsg", url.Values{
		"sn":      {sn},
		"content": {content},
		"times":   {strconv.Itoa(times)},
	})
	if err != nil {
		return "", err
	}
	var id string
	if err := json.Unmarshal(data, &id); err != nil {
		return "", fmt.Errorf("print/feie: Open_printMsg: order id: %w", err)
	}
	return id, nil
}

// Status implements Printer via Open_queryPrinterStatus. The vendor answers with
// a sentence: "离线。", "在线，工作状态正常。" or "在线，工作状态不正常。".
func (c *FeieClient) Status(ctx context.Context, sn string) (PrinterStatus, error) {
	data, err := c.call(ctx, "Open_queryPrinterStatus", url.Values{"sn": {sn}})
	if err != nil {
		return PrinterStatus{}, err
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return PrinterStatus{}, fmt.Errorf("print/feie: Open_queryPrinterStatus: %w", err)
	}
	online := strings.HasPrefix(text, "在线")
	return PrinterStatus{Online: online, Normal: online && !strings.Contains(text, "不正常"), Text: text}, nil
}

// Bind implements Printer via Open_printerAddlist (printerContent = sn#key#name).
// A printer that is already bound counts as success.
func (c *FeieClient) Bind(ctx context.Context, sn, key, name string) error {
	item := sn + "#" + key
	if name != "" {
		item += "#" + name
	}
	data, err := c.call(ctx, "Open_printerAddlist", url.Values{"printerContent": {item}})
	if err != nil {
		return err
	}
	var res struct {
		No []string `json:"no"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		return fmt.Errorf("print/feie: Open_printerAddlist: %w", err)
	}
	for _, msg := range res.No {
		if !strings.Contains(msg, "已被添加") {
			return fmt.Errorf("print/feie: Open_printerAddlist: %s", msg)
		}
	}
	return nil
}
