package physutil

import (
	"fmt"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/aif-go/agproto-spec/phys"
)

// RenderLine 定长：按 offset 定位写（§5.2）。
// 行长 = max(offset+length)；先 fill（空格）填充；skip/空值字段不写，位置保持 fill。
func RenderLine(layout *CachedLayout, msg proto.Message) ([]byte, error) {
	ref := msg.ProtoReflect()
	if layout.desc != ref.Descriptor() {
		return nil, fmt.Errorf("layout was built for %q, not %q", layout.desc.FullName(), ref.Descriptor().FullName())
	}
	var lineLen int32
	for _, cf := range layout.Fields() {
		if end := cf.Offset + cf.Length; end > lineLen {
			lineLen = end
		}
	}
	line := []byte(strings.Repeat(" ", int(lineLen)))
	for _, cf := range layout.Fields() {
		if cf.Skip {
			continue // 位置保持 fill
		}
		text := fieldText(ref, cf)
		if text == "" {
			continue // 空值/零值字符串：位置保持 fill
		}
		padded := padValue(text, cf.Length, cf.PadMode, cf.Fill)
		copy(line[cf.Offset:cf.Offset+cf.Length], padded)
	}
	return line, nil
}

// RenderDelimitedLine 分隔符：输出 index 1..maxIndex 全列（无字段列空占位），
// 按 index 升序 join；零值/skip → 空列（D11/D12）。
func RenderDelimitedLine(layout *CachedLayout, msg proto.Message) ([]byte, error) {
	ref := msg.ProtoReflect()
	if layout.desc != ref.Descriptor() {
		return nil, fmt.Errorf("layout was built for %q, not %q", layout.desc.FullName(), ref.Descriptor().FullName())
	}
	var maxIndex int32
	for _, cf := range layout.Fields() {
		if cf.Index > maxIndex {
			maxIndex = cf.Index
		}
	}
	cols := make([]string, 0, maxIndex)
	for idx := int32(1); idx <= maxIndex; idx++ {
		cf, ok := layout.FieldByIndex(idx)
		if !ok {
			cols = append(cols, "") // 无字段列 → 空占位
			continue
		}
		if cf.Skip {
			cols = append(cols, "")
			continue
		}
		text := fieldText(ref, cf)
		// D12：数值零值 → 空列
		if (cf.FieldDesc.Kind() == protoreflect.Int64Kind || cf.FieldDesc.Kind() == protoreflect.Int32Kind) && text == "0" {
			text = ""
		}
		if text == "" {
			cols = append(cols, "") // 零值 → 空列（D12）
			continue
		}
		cols = append(cols, text)
	}
	return []byte(strings.Join(cols, layout.Delimiter())), nil
}

// fieldText 取字段渲染文本（DECIMAL 规范化字符串原样返回，逆转换在调用方按模式处理）。
func fieldText(ref protoreflect.Message, cf CachedField) string {
	switch cf.FieldDesc.Kind() {
	case protoreflect.StringKind:
		s := ref.Get(cf.FieldDesc).String()
		if cf.Type == phys.FieldType_TYPE_DECIMAL && cf.DecimalMode != phys.DecimalMode_DECIMAL_MODE_EXPLICIT {
			return renderScaled(s) // SCALED：去小数点
		}
		return s
	case protoreflect.Int64Kind, protoreflect.Int32Kind:
		return strconv.FormatInt(ref.Get(cf.FieldDesc).Int(), 10)
	default:
		return ref.Get(cf.FieldDesc).String()
	}
}

// padValue 补位到 length（超长截断；PAD_LEFT 时符号前置后补 fill）。
func padValue(v string, length int32, padMode phys.PadMode, fill string) []byte {
	if fill == "" {
		fill = " "
	}
	if int32(len(v)) > length {
		v = v[:length] // 超长截断（B4）
	}
	for int32(len(v)) < length {
		switch padMode {
		case phys.PadMode_PAD_LEFT:
			// 符号保持前置："-123" + fill "0" → "-000123"
			if len(v) > 0 && (v[0] == '-' || v[0] == '+') {
				v = v[:1] + fill + v[1:]
			} else {
				v = fill + v
			}
		default: // PAD_RIGHT / 缺省
			v += fill
		}
	}
	return []byte(v)
}
