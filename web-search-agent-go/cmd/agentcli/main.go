// Command agentcli 是 Web Search Agent 的命令行入口，只做装配：
// 参数与 UI 全在 internal/interfaces，业务全在 application。
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/0226zy/magents/web-search-agent-go/internal/interfaces"
)

func main() {
	if err := interfaces.RunAgentCLI(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintf(os.Stderr, "\n❌ %v\n", err)
		os.Exit(1)
	}
}
