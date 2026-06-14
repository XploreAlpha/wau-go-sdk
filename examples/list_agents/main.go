// 示例:列出所有在线 agents
//
// 跑法:
//
//	cd examples/list_agents && go run main.go
//
// 期望:打印 3 个 agent(Whis/Jarvis/Benny)的 name / trust / status
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	wau "github.com/wau/wau-go-sdk"
)

func main() {
	c, err := wau.New("http://localhost:18400",
		wau.WithTimeout(10*time.Second),
		wau.WithRetryNo(), // list 不希望重试
	)
	if err != nil {
		log.Fatalf("wau.New: %v", err)
	}
	defer c.Close()

	agents, err := c.Agents().List(context.Background(), wau.PageOptions{PageSize: 10})
	if err != nil {
		log.Fatalf("List: %v", err)
	}
	fmt.Printf("在线 agents (%d):\n", len(agents.Agents))
	for _, a := range agents.Agents {
		fmt.Printf("  - %s  trust=%.2f  status=%s  skills=%v\n",
			a.Name, a.Trust, a.Status, a.Skills)
	}
}
