package bash

import "regexp"

// compileFilter 把用户提供的 filter 模式编译成 *regexp.Regexp。
// 与 bash_output 的 filter 字段对应；非法模式直接退化（filter 字段被忽略）。
func compileFilter(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, nil
	}
	return regexp.Compile(pattern)
}
