// 示例:agent 端定时心跳上报
//
// 模拟一个 agent 进程:每 60s 调一次 Heartbeat + 偶尔上报 load
//
// 跑法:
//
//	cd examples/heartbeat_loop && go run main.go my-agent
//
// 期望:每 60s 打印一次心跳日志
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	wau "github.com/wau/wau-go-sdk"
)

func main() {
	agentName := "demo-agent"
	if len(os.Args) > 1 {
		agentName = os.Args[1]
	}

	c, err := wau.New("http://localhost:18400",
		wau.WithTimeout(5*time.Second),
	)
	if err != nil {
		log.Fatalf("wau.New: %v", err)
	}
	defer c.Close()

	// 注册 agent
	err = c.Agents().Register(context.Background(), wau.AgentRegisterRequest{
		Name:        agentName,
		URL:         "http://demo-agent:18800",
		Description: "demo agent for heartbeat example",
		Skills:      []string{"demo", "test"},
	})
	if err != nil {
		log.Fatalf("Register: %v", err)
	}
	fmt.Printf("✅ Agent %q 已注册\n", agentName)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 监听 Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n[收到信号,正在退出...]")
		cancel()
	}()

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	// 立即跑一次
	doHeartbeat(ctx, c, agentName)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			doHeartbeat(ctx, c, agentName)
		}
	}
}

func doHeartbeat(ctx context.Context, c *wau.Client, agentID string) {
	if err := c.Agents().Heartbeat(ctx, agentID); err != nil {
		fmt.Printf("[%s] ❌ heartbeat: %v\n", time.Now().Format("15:04:05"), err)
		return
	}
	if err := c.Agents().ReportLoad(ctx, agentID, wau.AgentLoad{
		ActiveTasks: 0,
		MaxCapacity: 10,
		CPUUsage:    0.1,
		MemoryUsage: 0.2,
	}); err != nil {
		fmt.Printf("[%s] ⚠️  report load: %v\n", time.Now().Format("15:04:05"), err)
		return
	}
	fmt.Printf("[%s] 💓 heartbeat ok\n", time.Now().Format("15:04:05"))
}
