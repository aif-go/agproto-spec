package physutil

import (
	"fmt"
	"strings"
)

// DECIMAL 规范化（零第三方依赖，纯字符串 + 手写字符校验，无 int64 上限）。
// 输出恒定点数标准格式：[sign]int[.frac]（无前导 0，小数位恒等于 precision）。

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func stripSign(s string) (sign, digits string) {
	if len(s) > 0 && (s[0] == '-' || s[0] == '+') {
		return s[:1], s[1:]
	}
	return "", s
}

// normalizeScaled SCALED 解析：整数文本 → 恒定点数。
// "12345" p2 → "123.45"；"5" p2 → "0.05"；"-12345" → "-123.45"；全 0 → "0.00"。
func normalizeScaled(s string, precision int32) (string, error) {
	sign, digits := stripSign(s)
	if !allDigits(digits) {
		return "", fmt.Errorf("not an integer: %q", s)
	}
	digits = strings.TrimLeft(digits, "0")
	if digits == "" {
		digits = "0"
	}
	if precision == 0 {
		return sign + digits, nil
	}
	intPart := digits[:max(0, len(digits)-int(precision))]
	frac := digits[max(0, len(digits)-int(precision)):]
	if len(intPart) == 0 {
		intPart = "0"
	}
	for len(frac) < int(precision) {
		frac = "0" + frac
	}
	return sign + intPart + "." + frac, nil
}

// normalizeExplicit EXPLICIT 解析：带小数点（或无）→ 恒定点数。
// "123.45" p2 → "123.45"；"123.4" → "123.40"；"123" → "123.00"；小数位超 p → error。
func normalizeExplicit(s string, precision int32) (string, error) {
	sign, rest := stripSign(s)
	parts := strings.Split(rest, ".")
	if len(parts) > 2 {
		return "", fmt.Errorf("not a decimal: %q", s)
	}
	intPart, frac := parts[0], ""
	if len(parts) == 2 {
		frac = parts[1]
	}
	if !allDigits(intPart) || !allDigits(frac) {
		return "", fmt.Errorf("not a decimal: %q", s)
	}
	if len(frac) > int(precision) {
		return "", fmt.Errorf("decimal %q: fraction %d digits exceeds precision %d", s, len(frac), precision)
	}
	intPart = strings.TrimLeft(intPart, "0")
	if intPart == "" {
		intPart = "0"
	}
	for len(frac) < int(precision) {
		frac += "0"
	}
	return sign + intPart + "." + frac, nil
}

// renderScaled SCALED 渲染逆转换：恒定点数 → 整数文本（无小数点）。
// "123.45" → "12345"；"-0.05" → "-005"（符号保留，补位由 pad 阶段处理）。
func renderScaled(s string) string {
	return strings.ReplaceAll(s, ".", "")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
