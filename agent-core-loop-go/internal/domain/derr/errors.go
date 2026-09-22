// Package derr 存放领域哨兵错误：跨层判断用 errors.Is，不用字符串比较。
package derr

import "errors"

// ErrMaxIterations 表示达到循环上限仍未得到最终回答。
var ErrMaxIterations = errors.New("达到 max_iterations 上限，模型仍在请求工具调用")
