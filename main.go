package main

import (
	"context"
	"os"
	"runtime/trace"
	"time"
)

func main() {
	// 1. 创建 trace 输出文件
	f, _ := os.Create("trace.out")
	defer f.Close()
	trace.Start(f)
	defer trace.Stop() // 在程序结束时停止 trace

	ctx, cancel := context.WithCancel(context.Background())

	// 启动 5 个协程
	for i := 0; i < 5; i++ {
		go func() {
			<-ctx.Done() // 阻塞等待退出
		}()
	}

	time.Sleep(50 * time.Millisecond)
	cancel() // 触发乱序退出
	time.Sleep(50 * time.Millisecond)
}
