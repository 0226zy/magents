// Command experiment12 是实验 1-2 的命令行入口，只做装配：
// 批处理流程全在 internal/interfaces。
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/0226zy/magents/web-search-agent-go/internal/interfaces"
)

func main() {
	code, err := interfaces.RunExperiment12(os.Args[1:], os.Stdout, os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintf(os.Stderr, "\n❌ %v\n", err)
	}
	os.Exit(code)
}
