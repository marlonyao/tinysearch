package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"es/cluster"
	"es/server"
)

func main() {
	var (
		nodeID   = flag.String("node", "node-0", "Node ID")
		addr     = flag.String("http", ":9200", "HTTP listen address")
		seed     = flag.String("join", "", "Seed node ID to join (empty for bootstrap)")
	)
	flag.Parse()

	fmt.Printf("🔥 Starting ES node: %s\n", *nodeID)
	fmt.Printf("   HTTP: %s\n", *addr)
	if *seed == "" {
		fmt.Println("   Mode: Bootstrap (first node)")
	} else {
		fmt.Printf("   Mode: Join cluster via %s\n", *seed)
	}

	// 创建节点
	hub := cluster.NewTransportHub()
	transport := hub.CreateTransport(*nodeID)
	node := cluster.NewNode(*nodeID, transport)
	node.Start()

	// 加入集群
	if err := node.Join(*seed); err != nil {
		fmt.Printf("❌ Join failed: %v\n", err)
		os.Exit(1)
	}

	if *seed == "" {
		// 等待一下让节点状态稳定
		time.Sleep(100 * time.Millisecond)
		fmt.Println("✅ Bootstrap complete. This node is MASTER.")
	} else {
		fmt.Println("⏳ Waiting for cluster state sync...")
		if !node.WaitForStateVersion(1, 5*time.Second) {
			fmt.Println("⚠️  State sync timeout, node may not have joined correctly")
		} else {
			fmt.Println("✅ Cluster state synced.")
		}
	}

	// 启动 HTTP server
	srv := server.NewClusterServer(node)
	fmt.Printf("🚀 HTTP server starting on %s\n", *addr)
	
	// 优雅关闭
	go func() {
		if err := srv.ListenAndServe(*addr); err != nil {
			fmt.Printf("❌ HTTP server error: %v\n", err)
		}
	}()

	// 等待中断信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\n🛑 Shutting down...")
	node.Stop()
	fmt.Println("👋 Goodbye.")
}
