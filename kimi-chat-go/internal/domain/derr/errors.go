// Package derr 存放领域哨兵错误：跨层判断用 errors.Is，不用字符串比较。
package derr

import "errors"

// ErrNoChoices 表示模型响应中没有候选回答。
var ErrNoChoices = errors.New("响应中没有 choices")
