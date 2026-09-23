package physutil

import (
	"fmt"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/aif-go/agproto-spec/phys"
)

// Mapping 源 DTO → 标准 DTO 的映射表（BuildMapping 构建，运行时逐条执行）。
type Mapping struct {
	srcDesc protoreflect.MessageDescriptor
	dstDesc protoreflect.MessageDescriptor
	entries []MappingEntry
}

type MappingEntry struct {
	srcField CachedField
	dstField CachedField
	conv     converter
}

func (m *Mapping) Entries() []MappingEntry { return m.entries }

// converter 两层：validate（构建时字段级规则）+ convert（运行时执行）。
type converter interface {
	validate(e MappingEntry) error
	convert(srcText string, e MappingEntry) (string, error)
}

// BuildMapping 读源布局的 target 注解，建立映射并校验（§5.3/§3.5 规则 12）。
func BuildMapping(srcL, dstL *CachedLayout) (*Mapping, error) {
	m := &Mapping{srcDesc: srcL.desc, dstDesc: dstL.desc}
	for _, sf := range srcL.fields {
		if sf.Target == "" || sf.Skip {
			continue
		}
		df, ok := dstL.Field(sf.Target)
		if !ok {
			return nil, fmt.Errorf("field %q: target %q not found in destination", sf.FieldName, sf.Target)
		}
		entry := MappingEntry{srcField: sf, dstField: df}
		conv, err := lookupConverter(entry)
		if err != nil {
			return nil, fmt.Errorf("field %q → %q: %w", sf.FieldName, sf.Target, err)
		}
		if err := conv.validate(entry); err != nil {
			return nil, fmt.Errorf("field %q → %q: %w", sf.FieldName, sf.Target, err)
		}
		entry.conv = conv
		m.entries = append(m.entries, entry)
	}
	return m, nil
}

// MapToStandard 执行映射：源值文本 → 转换器 → 目标字段 Set。
func MapToStandard(m *Mapping, src, dst proto.Message) error {
	srcRef := src.ProtoReflect()
	dstRef := dst.ProtoReflect()
	if m.srcDesc != srcRef.Descriptor() || m.dstDesc != dstRef.Descriptor() {
		return fmt.Errorf("mapping was built for %q → %q, not %q → %q",
			m.srcDesc.FullName(), m.dstDesc.FullName(),
			srcRef.Descriptor().FullName(), dstRef.Descriptor().FullName())
	}
	for _, e := range m.entries {
		text := srcText(srcRef, e.srcField)
		out, err := e.conv.convert(text, e)
		if err != nil {
			return fmt.Errorf("field %q → %q: %w", e.srcField.FieldName, e.dstField.FieldName, err)
		}
		setText(dstRef, e.dstField, out)
	}
	return nil
}

// lookupConverter 按类型路由到转换器（kind 白名单 + DECIMAL 路由）。
func lookupConverter(e MappingEntry) (converter, error) {
	src, dst := e.srcField, e.dstField

	// DECIMAL 参与（任一侧）：两字段必须 string kind
	if src.Type == phys.FieldType_TYPE_DECIMAL || dst.Type == phys.FieldType_TYPE_DECIMAL {
		if src.FieldDesc.Kind() != protoreflect.StringKind || dst.FieldDesc.Kind() != protoreflect.StringKind {
			return nil, fmt.Errorf("decimal mapping requires string fields on both sides")
		}
		return decimalConverter{}, nil
	}

	switch src.FieldDesc.Kind() {
	case protoreflect.StringKind:
		switch dst.FieldDesc.Kind() {
		case protoreflect.StringKind:
			return copyConverter{}, nil
		case protoreflect.Int64Kind, protoreflect.Int32Kind:
			return strToIntConverter{}, nil
		}
	case protoreflect.Int64Kind, protoreflect.Int32Kind:
		switch dst.FieldDesc.Kind() {
		case protoreflect.StringKind:
			return copyConverter{}, nil
		case protoreflect.Int64Kind, protoreflect.Int32Kind:
			return copyConverter{}, nil
		}
	}
	return nil, fmt.Errorf("illegal type mapping %v → %v", src.FieldDesc.Kind(), dst.FieldDesc.Kind())
}

// ── 转换器实现 ──

type copyConverter struct{}

func (copyConverter) validate(MappingEntry) error { return nil }
func (copyConverter) convert(src string, _ MappingEntry) (string, error) {
	return src, nil
}

type strToIntConverter struct{}

func (strToIntConverter) validate(_ MappingEntry) error { return nil }
func (strToIntConverter) convert(src string, _ MappingEntry) (string, error) {
	if _, err := strconv.ParseInt(strings.TrimSpace(src), 10, 64); err != nil {
		return "", fmt.Errorf("not an integer: %q", src)
	}
	return src, nil
}

type decimalConverter struct{}

func (decimalConverter) validate(e MappingEntry) error {
	if e.srcField.Type != phys.FieldType_TYPE_DECIMAL || e.dstField.Type != phys.FieldType_TYPE_DECIMAL {
		return nil // 一侧普通 string：拷贝，格式业务验证
	}
	switch {
	case e.srcField.Precision == e.dstField.Precision:
		return nil
	case e.srcField.Precision < e.dstField.Precision:
		return nil // 升精度：无损补零
	default:
		return fmt.Errorf("lossy precision %d→%d (align proto or add explicit conversion)",
			e.srcField.Precision, e.dstField.Precision)
	}
}

func (decimalConverter) convert(src string, e MappingEntry) (string, error) {
	if e.srcField.Type == phys.FieldType_TYPE_DECIMAL && e.dstField.Type == phys.FieldType_TYPE_DECIMAL &&
		e.srcField.Precision < e.dstField.Precision {
		// 升精度：小数部分补零
		frac := int(e.dstField.Precision)
		if i := strings.IndexByte(src, '.'); i >= 0 {
			frac -= len(src) - i - 1
			if frac < 0 {
				return "", fmt.Errorf("invalid scaled decimal: %q", src)
			}
			return src + strings.Repeat("0", frac), nil
		}
		return src + "." + strings.Repeat("0", int(e.dstField.Precision)), nil
	}
	return src, nil
}

// srcText 取源字段值文本（DECIMAL 保持规范化字符串，不做 SCALED 逆转换）。
func srcText(ref protoreflect.Message, cf CachedField) string {
	switch cf.FieldDesc.Kind() {
	case protoreflect.StringKind:
		return ref.Get(cf.FieldDesc).String()
	case protoreflect.Int64Kind, protoreflect.Int32Kind:
		return strconv.FormatInt(ref.Get(cf.FieldDesc).Int(), 10)
	default:
		return ref.Get(cf.FieldDesc).String()
	}
}

// setText 按目标字段 kind 写入。
func setText(ref protoreflect.Message, cf CachedField, text string) {
	switch cf.FieldDesc.Kind() {
	case protoreflect.StringKind:
		ref.Set(cf.FieldDesc, protoreflect.ValueOfString(text))
	case protoreflect.Int64Kind:
		if n, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64); err == nil {
			ref.Set(cf.FieldDesc, protoreflect.ValueOfInt64(n))
		} else {
			ref.Set(cf.FieldDesc, protoreflect.ValueOfInt64(0)) // 非数字 → 零值（有值模型）
		}
	case protoreflect.Int32Kind:
		if n, err := strconv.ParseInt(strings.TrimSpace(text), 10, 32); err == nil {
			ref.Set(cf.FieldDesc, protoreflect.ValueOfInt32(int32(n)))
		} else {
			ref.Set(cf.FieldDesc, protoreflect.ValueOfInt32(0))
		}
	default:
		ref.Set(cf.FieldDesc, protoreflect.ValueOfString(text))
	}
}
