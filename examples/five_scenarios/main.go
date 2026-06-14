// 示例:跑 5 场景契约(模拟 wau-intent/e2e_test/test_submit_l4.py)
//
// 跑法:
//
//	cd examples/five_scenarios && go run main.go
//
// 期望:5/5 通过,跟真 kernel e2e 行为一致
package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	wau "github.com/wau/wau-go-sdk"
)

var scenarios = []struct {
	scene            string
	prompt           string
	expectedAgent    string
	mustHaveTokens   []string
}{
	{"clinical", "I need clinical decision support for a patient", "Jarvis",
		[]string{"临床", "决策", "支持", "患者"}},
	{"france", "What is the capital of France?", "Whis",
		[]string{"paris"}},
	{"pain", "Recommend an over-the-counter pain reliever", "Benny",
		[]string{"ibuprofen", "acetaminophen"}},
	{"sales", "Show me this quarter's sales analytics", "Whis",
		[]string{"sales", "analytics", "quarter"}},
	{"rare_disease", "Help me diagnose a rare disease", "Jarvis",
		[]string{"罕见病", "鉴别", "诊断"}},
}

func main() {
	c, err := wau.New("http://localhost:18400",
		wau.WithTimeout(30*time.Second),
	)
	if err != nil {
		log.Fatalf("wau.New: %v", err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pass, fail := 0, 0
	for _, sc := range scenarios {
		fmt.Printf("\n=== %s ===\n", sc.scene)
		fmt.Printf("Prompt: %s\n", sc.prompt)

		resp, err := c.Tasks().Submit(ctx, wau.SubmitRequest{
			Prompt:    sc.prompt,
			TimeoutMs: 60000,
		})
		if err != nil {
			fmt.Printf("   ❌ HTTP error: %v\n", err)
			fail++
			continue
		}
		if resp.Status != "completed" {
			fmt.Printf("   ❌ status=%s err=%s\n", resp.Status, resp.Error)
			fail++
			continue
		}
		if resp.SelectedAgent != sc.expectedAgent {
			fmt.Printf("   ❌ 选了 %s (期望 %s)\n", resp.SelectedAgent, sc.expectedAgent)
			fail++
			continue
		}
		text := strings.ToLower(fmt.Sprintf("%v", resp.Response))
		matched := false
		for _, tok := range sc.mustHaveTokens {
			if strings.Contains(text, strings.ToLower(tok)) {
				matched = true
				break
			}
		}
		if !matched {
			fmt.Printf("   ❌ 响应里没找到期望 token\n")
			fail++
			continue
		}
		fmt.Printf("   ✅ → %s  L3=%dms A2A=%dms\n",
			resp.SelectedAgent, resp.Decision.DecisionTimeMs, resp.A2ACallMs)
		pass++
	}
	fmt.Printf("\n=== 汇总: %d/%d 通过 ===\n", pass, len(scenarios))
}
