package main

// Slot-choice probe (LLM=1 SLOTS=probe): does the model file each statement
// under the right attribute? Replacement only works when two values of one
// attribute land in the same slot, so a wrong-but-inconsistent choice loses
// the replacement. Frozen before any change to the slot descriptions; dev and
// held-out written together. Nothing is judged from the answer text — only
// the remember_customer_fact calls and their results.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	repoai "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/ai"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
	domainai "github.com/hanmahong5-arch/lurus-tally/internal/domain/ai"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/memorusclient"
)

// slotProbe: every attribute in want must be called (each with one of its
// accepted slot ids), filed under customer.
type slotProbe struct {
	text     string
	customer string
	want     [][]string // one entry per attribute; accepted slot ids
}

func one(ids ...string) []string { return ids }

func devSlotProbes() []slotProbe {
	return []slotProbe{
		{"王五那边以后货直接送到他后门仓库", "王五", [][]string{one("delivery_address")}},
		{"孙七以后不用送了，他自己开车来提", "孙七", [][]string{one("delivery_method")}},
		{"赵六的货以后走顺丰快递", "赵六", [][]string{one("delivery_method")}},
		{"张三丰那边每个月底对一次账再付", "张三丰", [][]string{one("settlement")}},
		{"李四现在都用支付宝扫码", "李四", [][]string{one("payment_method")}},
		{"王五要求早上九点前送到", "王五", [][]string{one("delivery_time")}},
		{"孙七说以后找他们店长小刘收货", "孙七", [][]string{one("contact")}},
		{"张三的饮料给他打九折", "张三", [][]string{one("price_terms")}},
		{"赵六不吃辣", "赵六", [][]string{one("allergy", "taste")}},
		{"李四每次都要两箱矿泉水", "李四", [][]string{one("regular_items")}},
		{"王五改用微信付款了，以后送货改到下午", "王五", [][]string{one("payment_method"), one("delivery_time")}},
		{"老李的发票抬头改成李记商行", "李四", [][]string{one("invoice")}},
		{"张三喜欢喝无糖的", "张三", [][]string{one("taste")}},
		{"赵六的货要用泡沫箱装，怕压", "赵六", [][]string{one("packaging")}},
	}
}

func holdoutSlotProbes() []slotProbe {
	return []slotProbe{
		{"周八的货以后放到她家楼下的快递柜", "周八", [][]string{one("delivery_address")}},
		{"吴九以后自己来店里拿货", "吴九", [][]string{one("delivery_method")}},
		{"刘二那边改成每半个月结一次", "刘二", [][]string{one("settlement")}},
		{"陈一鸣付钱都用银行转账", "陈一鸣", [][]string{one("payment_method")}},
		{"郑十要求周末别送货", "郑十", [][]string{one("delivery_time")}},
		{"陈一的货交给他女婿签收", "陈一", [][]string{one("contact")}},
		{"吴九买油每桶便宜五块", "吴九", [][]string{one("price_terms")}},
		{"周八对海鲜过敏", "周八", [][]string{one("allergy")}},
		{"刘二每周都要一箱鸡蛋", "刘二", [][]string{one("regular_items")}},
		{"郑十以后刷卡付，货装纸箱就行", "郑十", [][]string{one("payment_method"), one("packaging")}},
		{"吴老板的发票要开专票", "吴九", [][]string{one("invoice")}},
		{"陈一鸣喜欢老抽不喜欢生抽", "陈一鸣", [][]string{one("taste")}},
		{"刘二的货用拉货的三轮车送", "刘二", [][]string{one("delivery_method")}},
		{"周八的店搬到了人民路", "周八", [][]string{one("delivery_address")}},
	}
}

func runSlotProbe(ctx context.Context, appDB dbHandle, sales *repoai.SQLSaleRepo, tenant uuid.UUID, label string) {
	probes := devSlotProbes()
	if label == "holdout" {
		probes = holdoutSlotProbes()
	}
	llm, err := llmclient.New(llmclient.Config{
		BaseURL: os.Getenv("NEWAPI_BASE_URL"), APIKey: os.Getenv("NEWAPI_API_KEY"),
		HTTPTimeout: 120 * time.Second,
	})
	must(err, "llm client")
	mc, err := memorusclient.New(memorusclient.Config{BaseURL: os.Getenv("MEMORUS_URL"), APIKey: "probekey"})
	if err != nil || mc == nil {
		panic(fmt.Sprint("memorus client: ", err))
	}
	reg := ai.NewRegistry(repoai.NewSQLProductRepo(appDB.db), repoai.NewSQLStockRepo(appDB.db), sales,
		repoai.NewSQLExchangeRateRepo(appDB.db))
	model := os.Getenv("MODEL")
	o := ai.NewOrchestrator(llm, reg, &memPlans{m: map[uuid.UUID]*domainai.Plan{}}, model).
		WithMemory(mc).WithCustomerResolver(sales)

	allRight, called, saved, wantAttrs, gotAttrs, extra := 0, 0, 0, 0, 0, 0
	for _, p := range probes {
		out, err := o.Chat(ctx, ai.ChatInput{TenantID: tenant, UserMessage: p.text})
		if err != nil {
			fmt.Printf("PROBE err=%v q=%q\n", err, p.text)
			continue
		}
		var slots []string
		savedAll, anyCall := true, false
		for _, t := range out.ToolCalls {
			if t.ToolName != "remember_customer_fact" {
				continue
			}
			anyCall = true
			var args struct {
				Attribute string `json:"attribute"`
			}
			_ = json.Unmarshal([]byte(t.ArgsJSON), &args)
			slots = append(slots, args.Attribute)
			var res struct {
				Saved    bool   `json:"saved"`
				Customer string `json:"customer"`
			}
			_ = json.Unmarshal([]byte(t.ResultJSON), &res)
			savedAll = savedAll && res.Saved && res.Customer == p.customer
			fmt.Printf("  call %s -> %s\n", t.ArgsJSON, t.ResultJSON)
		}
		if anyCall {
			called++
		}
		if anyCall && savedAll {
			saved++
		}
		// Match each wanted attribute to a distinct call.
		used := make([]bool, len(slots))
		matched := 0
		for _, acc := range p.want {
			for i, s := range slots {
				if !used[i] && contains(acc, s) {
					used[i] = true
					matched++
					break
				}
			}
		}
		wantAttrs += len(p.want)
		gotAttrs += matched
		for _, u := range used {
			if !u {
				extra++
			}
		}
		ok := matched == len(p.want) && anyCall && savedAll
		if ok {
			allRight++
		}
		sort.Strings(slots)
		fmt.Printf("PROBE ok=%v want=%v got=%v q=%q\n", ok, p.want, slots, p.text)
		time.Sleep(time.Second)
	}
	fmt.Printf("SCORE slotprobe scenario=%s model=%s | statements_all_right %d/%d | attributes_right %d/%d | extra_calls %d | tool_called %d/%d | saved_to_right_customer %d/%d\n",
		label, modelName(model), allRight, len(probes), gotAttrs, wantAttrs, extra, called, len(probes), saved, len(probes))
}

func contains(ss []string, s string) bool {
	return strings.Contains(" "+strings.Join(ss, " ")+" ", " "+s+" ")
}
