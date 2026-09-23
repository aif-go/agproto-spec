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
// 编码时机：先编码后 pad/截断（length 是目标编码字节宽度）。
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
			continue // skip 字段：位置保持空白（不参与输出）
		}
		text := fieldText(ref, cf)
		if text == "" {
			// 空值/零值：位置用 fill 占位（fill 缺省空格；PAD_LEFT+fill="0" → 全 0）
			if fillByte := fillOf(cf); fillByte != ' ' {
				for i := cf.Offset; i < cf.Offset+cf.Length; i++ {
					line[i] = fillByte
				}
			}
			continue
		}
		b, err := layout.encoder.Bytes([]byte(text))
		if err != nil {
			return nil, fmt.Errorf("field %q: encode: %w", cf.FieldName, err)
		}
		padded := padBytes(b, cf.Length, cf.PadMode, cf.Fill)
		copy(line[cf.Offset:cf.Offset+cf.Length], padded)
	}
	return line, nil
}

// fillOf 返回字段的填充字符（缺省空格）。
func fillOf(cf CachedField) byte {
	if cf.Fill == "" {
		return ' '
	}
	return cf.Fill[0]
}

// RenderDelimitedLine 分隔符：输出 index 1..maxIndex 全列（无字段列空占位），
// 按 index 升序 join（UTF-8）；零值/skip → 空列（D11/D12）；整行编码输出。
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
	out := []byte(strings.Join(cols, layout.Delimiter()))
	return layout.encoder.Bytes(out)
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

// padBytes 补位到 length（编码后字节，超长截断；PAD_LEFT 时符号前置后补 fill）。
func padBytes(v []byte, length int32, padMode phys.PadMode, fill string) []byte {
	fillByte := byte(' ')
	if fill != "" {
		fillByte = fill[0]
	}
	if int32(len(v)) > length {
		v = v[:length] // 编码后字节截断（B4）
	}
	for int32(len(v)) < length {
		switch padMode {
		case phys.PadMode_PAD_LEFT:
			// 符号保持前置："-123" + fill '0' → "-000123"
			if len(v) > 0 && (v[0] == '-' || v[0] == '+') {
				v = append(append([]byte{v[0]}, fillByte), v[1:]...)
			} else {
				v = append([]byte{fillByte}, v...)
			}
		default: // PAD_RIGHT / 缺省
			v = append(v, fillByte)
		}
	}
	return v
}
