package physutil

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/aif-go/agproto-spec/phys"
)

// ParseLine 定长行 → DTO：按 CachedField 切字节、解码、trim、kind 转换、Set。
// 有值模型：空内容 Set 类型零值（非 nil）；行短 → error（B1）。
// 编码时机：先切后解码（offset/length 是源编码字节宽度）。
func ParseLine(layout *CachedLayout, dest proto.Message, line []byte) error {
	ref := dest.ProtoReflect()
	if layout.desc != ref.Descriptor() {
		return fmt.Errorf("layout was built for %q, not %q", layout.desc.FullName(), ref.Descriptor().FullName())
	}
	for _, cf := range layout.fields {
		end := cf.Offset + cf.Length
		if end > int32(len(line)) {
			return fmt.Errorf("field %q: line too short (%d bytes), need offset %d + length %d = %d",
				cf.FieldName, len(line), cf.Offset, cf.Length, end)
		}
		raw := line[cf.Offset:end]
		dec, err := layout.decoder.Bytes(raw)
		if err != nil {
			return fmt.Errorf("field %q: decode: %w", cf.FieldName, err)
		}
		if err := checkFFFD(dec); err != nil {
			return fmt.Errorf("field %q: %w", cf.FieldName, err)
		}
		if err := setField(ref, cf, dec); err != nil {
			return err
		}
	}
	return nil
}

// ParseDelimitedLine 分隔符行 → DTO：整行解码后 Split（UTF-8 多字节不含 ASCII delimiter 字节，
// GBK 双字节第二字节可含 0x7C——原始字节 Split 会错切）。
func ParseDelimitedLine(layout *CachedLayout, dest proto.Message, line []byte) error {
	dec, err := layout.decoder.Bytes(line)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if err := checkFFFD(dec); err != nil {
		return err
	}
	parts := strings.Split(string(dec), layout.Delimiter())
	ref := dest.ProtoReflect()
	if layout.desc != ref.Descriptor() {
		return fmt.Errorf("layout was built for %q, not %q", layout.desc.FullName(), ref.Descriptor().FullName())
	}
	for _, cf := range layout.fields {
		if int(cf.Index) > len(parts) {
			return fmt.Errorf("field %q: index %d exceeds column count %d", cf.FieldName, cf.Index, len(parts))
		}
		if err := setField(ref, cf, []byte(parts[cf.Index-1])); err != nil {
			return err
		}
	}
	return nil
}

// checkFFFD 解码后检测 U+FFFD 替换符（x/text 对非法字节/无法映射字节替换为 U+FFFD，
// 检测 → error 等效 fail-fast。合法内容含 U+FFFD 字符会误报——银行文件概率≈0，文档注明）。
func checkFFFD(dec []byte) error {
	if bytes.Contains(dec, []byte(string(utf8.RuneError))) {
		return fmt.Errorf("decoded bytes contain U+FFFD (encoding mismatch?)")
	}
	return nil
}

// CheckFFFD 导出版（消费方编码校验场景用）。
func CheckFFFD(dec []byte) error { return checkFFFD(dec) }

// setField 单字段：trim → 空则 Set 零值 → 按 kind/type 转换 Set。
func setField(ref protoreflect.Message, cf CachedField, raw []byte) error {
	var val string
	if cf.Type == phys.FieldType_TYPE_DECIMAL {
		// DECIMAL：仅去两侧空白——前导 0 是有效数字（"0.56" 不能被 0-trim 破坏），
		// 规范化器内部统一去前导 0/补零；全 0（无小数点）= 全 fill → 空白（有值模型）
		val = strings.TrimSpace(string(raw))
		if val != "" && strings.Trim(val, "0") == "" {
			val = ""
		}
	} else {
		val = trimByPad(string(raw), cf.PadMode, cf.Fill)
	}
	if val == "" {
		// 有值模型：空内容 Set 类型零值（非 nil）
		setZero(ref, cf)
		return nil
	}

	switch cf.FieldDesc.Kind() {
	case protoreflect.StringKind:
		switch cf.Type {
		case phys.FieldType_TYPE_DECIMAL:
			var norm string
			var err error
			if cf.DecimalMode == phys.DecimalMode_DECIMAL_MODE_EXPLICIT {
				norm, err = normalizeExplicit(val, cf.Precision)
			} else {
				norm, err = normalizeScaled(val, cf.Precision)
			}
			if err != nil {
				return fmt.Errorf("field %q: %w", cf.FieldName, err)
			}
			ref.Set(cf.FieldDesc, protoreflect.ValueOfString(norm))
		default: // STRING / DATE：原样（DATE 宽度已构建期校验）
			ref.Set(cf.FieldDesc, protoreflect.ValueOfString(val))
		}
	case protoreflect.Int64Kind:
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return fmt.Errorf("field %q: not an integer: %q", cf.FieldName, val)
		}
		ref.Set(cf.FieldDesc, protoreflect.ValueOfInt64(n))
	case protoreflect.Int32Kind:
		n, err := strconv.ParseInt(val, 10, 32)
		if err != nil {
			return fmt.Errorf("field %q: not an integer: %q", cf.FieldName, val)
		}
		ref.Set(cf.FieldDesc, protoreflect.ValueOfInt32(int32(n)))
	default:
		return fmt.Errorf("field %q: unsupported kind %v", cf.FieldName, cf.FieldDesc.Kind())
	}
	return nil
}

// setZero 有值模型：空内容 Set 类型零值（非 nil）
func setZero(ref protoreflect.Message, cf CachedField) {
	ref.Set(cf.FieldDesc, zeroValue(cf))
}

// zeroValue 类型零值
func zeroValue(cf CachedField) protoreflect.Value {
	switch cf.FieldDesc.Kind() {
	case protoreflect.Int64Kind:
		return protoreflect.ValueOfInt64(0)
	case protoreflect.Int32Kind:
		return protoreflect.ValueOfInt32(0)
	default:
		return protoreflect.ValueOfString("")
	}
}

// trimByPad 按 pad/fill 修剪填充字符；pad 缺省 = PAD_RIGHT（空格，定长文件最常见）。
// ⚠️ 已知限制（H2）：fill 是 cutset（字符集合），PAD_LEFT+fill="0" 会去掉数值/卡号字段的
// 填充前导 0（期望行为），但也会误伤"值本身以 0 开头且无填充"的 string 字段
// （如 length=4 值 "0123" 文件 "0123" → 解析得 "123"）。
// 约定：前导 0 有意义的字段禁用 fill="0"（用空格填充或 PAD_RIGHT）；fill="0" 仅用于数值/DECIMAL。
func trimByPad(s string, padMode phys.PadMode, fill string) string {
	if fill == "" {
		fill = " "
	}
	switch padMode {
	case phys.PadMode_PAD_LEFT:
		return strings.TrimLeft(s, fill)
	case phys.PadMode_PAD_RIGHT, phys.PadMode_PAD_UNSPECIFIED:
		return strings.TrimRight(s, fill)
	default:
		return s
	}
}
