package physutil

import (
	"sync"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
)

// 编码注册表：名字 → x/text encoding.Encoding（转换能力全在 x/text，本包只做分发）。
// 内置银行常用编码；消费方可 RegisterEncoding 扩展（UTF-16/Big5 等）。
var (
	encodingsMu sync.RWMutex
	encodings   = map[string]encoding.Encoding{
		"UTF-8":   unicode.UTF8,
		"GBK":     simplifiedchinese.GBK,
		"GB18030": simplifiedchinese.GB18030,
	}
)

// RegisterEncoding 注册编码实现（启动期调用，须在解析前完成）。
func RegisterEncoding(name string, e encoding.Encoding) {
	encodingsMu.Lock()
	defer encodingsMu.Unlock()
	encodings[name] = e
}

// LookupEncoding 按名查编码。
func LookupEncoding(name string) (encoding.Encoding, bool) {
	encodingsMu.RLock()
	defer encodingsMu.RUnlock()
	e, ok := encodings[name]
	return e, ok
}
