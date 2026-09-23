// Package physutil 是 phys 注解的运行时实现：布局构建、解析、渲染、映射。
// 仅依赖 google.golang.org/protobuf + stdlib（零第三方）。
package physutil

import (
	"fmt"
	"sort"

	"golang.org/x/text/encoding"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/aif-go/agproto-spec/phys"
)

// CachedField 描述一个字段的布局信息（从 phys.FieldLayout 扩展预计算而来）。
type CachedField struct {
	Offset       int32            // 定长：字节偏移（显式或 index 推算）
	Length       int32            // 定长：字节宽度
	Index        int32            // 字段序号（定长=第几段 / 分隔符=第几列）
	PadMode      phys.PadMode     // 对齐模式
	Fill         string           // 填充字符（空 = 空格）
	Target       string           // Map：目标字段名
	Skip         bool             // Map：跳过
	Type         phys.FieldType   // 类型语义（缺省 = 跟随 proto 字段类型）
	DecimalMode  phys.DecimalMode // DECIMAL 存储模式（缺省 SCALED）
	Precision    int32            // DECIMAL 小数位
	HasPrecision bool             // precision 是否显式声明（presence）
	DatePattern  phys.DatePattern // DATE 格式
	FieldName    string           // proto 字段名
	FieldDesc    protoreflect.FieldDescriptor
}

// CachedLayout 描述一个消息的布局（构建时预计算 + 校验）。
type CachedLayout struct {
	desc      protoreflect.MessageDescriptor
	format    phys.SourceFormat
	delimiter string
	encoding  string
	decoder   *encoding.Decoder // 缓存单例（x/text 无状态，并发安全已验证）
	encoder   *encoding.Encoder
	fields    []CachedField
	byName    map[string]int
	byIndex   map[int32]int
}

func (l *CachedLayout) Format() phys.SourceFormat { return l.format }
func (l *CachedLayout) Delimiter() string         { return l.delimiter }
func (l *CachedLayout) Encoding() string          { return l.encoding }
func (l *CachedLayout) Fields() []CachedField     { return l.fields }
func (l *CachedLayout) Field(name string) (CachedField, bool) {
	i, ok := l.byName[name]
	if !ok {
		return CachedField{}, false
	}
	return l.fields[i], true
}
func (l *CachedLayout) FieldByIndex(idx int32) (CachedField, bool) {
	i, ok := l.byIndex[idx]
	if !ok {
		return CachedField{}, false
	}
	return l.fields[i], true
}

// 日期枚举宽度（字符数）
func dateWidth(p phys.DatePattern) (int32, bool) {
	switch p {
	case phys.DatePattern_DATE_PATTERN_YYYYMMDD:
		return 8, true
	case phys.DatePattern_DATE_PATTERN_HHMMSS:
		return 6, true
	case phys.DatePattern_DATE_PATTERN_YYYYMMDDHHMMSS:
		return 14, true
	default:
		return 0, false
	}
}

// BuildCachedLayout 从消息描述符构建布局：读取注解、校验规则（§3.5）、
// 按 index 排序并推算 offset（定长）、建立字段名索引。
func BuildCachedLayout(desc protoreflect.MessageDescriptor) (*CachedLayout, error) {
	layout := &CachedLayout{
		desc:      desc,
		format:    phys.SourceFormat_FORMAT_FIXED, // 缺省定长
		delimiter: "",
		byName:    make(map[string]int),
	}

	// 编码解析：查注册表（缺省 UTF-8），未知 → 构建报错（fail-fast）
	enc := "UTF-8"
	if mdOpts := desc.Options(); mdOpts != nil && proto.HasExtension(mdOpts, phys.E_File) {
		ff := proto.GetExtension(mdOpts, phys.E_File).(*phys.FileFormat)
		if ff.GetFormat() != phys.SourceFormat_FORMAT_UNSPECIFIED {
			layout.format = ff.GetFormat()
		}
		layout.delimiter = ff.GetDelimiter()
		if ff.GetEncoding() != "" {
			enc = ff.GetEncoding()
		}
	}
	e, ok := LookupEncoding(enc)
	if !ok {
		return nil, fmt.Errorf("unsupported encoding %q (register via RegisterEncoding)", enc)
	}
	layout.encoding = enc
	layout.decoder = e.NewDecoder() // 缓存单例：构建时一次，逐行零分配
	layout.encoder = e.NewEncoder()

	// 分隔符格式必须声明非空 delimiter（空串 Split 会按 rune 切分，静默错乱）
	if layout.format == phys.SourceFormat_FORMAT_DELIMITED && layout.delimiter == "" {
		return nil, fmt.Errorf("delimited format requires non-empty delimiter")
	}

	fields := desc.Fields()
	raw := make([]CachedField, 0, fields.Len())
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		lf := getLayoutSafe(fd)
		if lf == nil {
			continue // 无 layout 注解的字段不参与
		}
		cf, err := buildField(fd, lf, layout.format)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", fd.Name(), err)
		}
		raw = append(raw, cf)
	}

	// index 缺省 = 声明顺序（1-based）
	for i := range raw {
		if raw[i].Index == 0 {
			raw[i].Index = int32(i + 1)
		}
	}

	// index 唯一性
	seen := map[int32]string{}
	for _, cf := range raw {
		if prev, ok := seen[cf.Index]; ok {
			return nil, fmt.Errorf("duplicate index %d: fields %q and %q", cf.Index, prev, cf.FieldName)
		}
		seen[cf.Index] = cf.FieldName
	}

	// 按 index 排序（渲染/推算共用顺序）
	sort.Slice(raw, func(i, j int) bool { return raw[i].Index < raw[j].Index })

	// 定长：推算 offset + 重叠/边界校验
	if layout.format == phys.SourceFormat_FORMAT_FIXED {
		var prevEnd int32
		for i := range raw {
			if raw[i].Offset < 0 { // 未显式 offset → 前字段尾部
				raw[i].Offset = prevEnd
			}
			if raw[i].Offset < prevEnd {
				return nil, fmt.Errorf("field %q: overlaps previous field (start %d < prev end %d)", raw[i].FieldName, raw[i].Offset, prevEnd)
			}
			prevEnd = raw[i].Offset + raw[i].Length
		}
	}

	layout.fields = raw
	layout.byIndex = make(map[int32]int, len(raw))
	for i, cf := range raw {
		layout.byName[cf.FieldName] = i
		layout.byIndex[cf.Index] = i
	}
	return layout, nil
}

func getLayoutSafe(fd protoreflect.FieldDescriptor) *phys.FieldLayout {
	opts := fd.Options()
	if opts != nil && proto.HasExtension(opts, phys.E_Layout) {
		return proto.GetExtension(opts, phys.E_Layout).(*phys.FieldLayout)
	}
	return nil
}

// buildField 单字段校验 + 构建（§3.5 规则 1-6、9-11）
func buildField(fd protoreflect.FieldDescriptor, lf *phys.FieldLayout, format phys.SourceFormat) (CachedField, error) {
	cf := CachedField{
		Offset:       -1, // 哨兵：未显式设置（构建时推算）
		Length:       lf.GetLength(),
		Index:        lf.GetIndex(),
		PadMode:      lf.GetPad(),
		Fill:         lf.GetFill(),
		Target:       lf.GetTarget(),
		Skip:         lf.GetSkip(),
		Type:         lf.GetType(),
		DecimalMode:  lf.GetDecimalMode(),
		Precision:    lf.GetPrecision(),
		HasPrecision: lf.Precision != nil, // proto3 optional presence
		DatePattern:  lf.GetDatePattern(),
		FieldName:    string(fd.Name()),
		FieldDesc:    fd,
	}
	if lf.Offset != nil {
		cf.Offset = *lf.Offset
		if cf.Offset < 0 {
			return cf, fmt.Errorf("negative offset %d", cf.Offset)
		}
	}
	if lf.Index != nil {
		cf.Index = *lf.Index
		if cf.Index <= 0 {
			return cf, fmt.Errorf("index must be > 0 (got %d)", cf.Index)
		}
	}
	if cf.DecimalMode == phys.DecimalMode_DECIMAL_MODE_UNSPECIFIED {
		cf.DecimalMode = phys.DecimalMode_DECIMAL_MODE_SCALED
	}

	// 规则 6：bool 字段禁止
	if fd.Kind() == protoreflect.BoolKind {
		return cf, fmt.Errorf("bool fields are not supported (use string)")
	}

	// 支持 kind 白名单：string / int64 / int32（enum 等其他 kind 构建期拒绝）
	switch fd.Kind() {
	case protoreflect.StringKind, protoreflect.Int64Kind, protoreflect.Int32Kind:
	default:
		return cf, fmt.Errorf("unsupported kind %v for phys layout", fd.Kind())
	}

	// 规则 1：type ↔ proto 字段类型
	switch cf.Type {
	case phys.FieldType_TYPE_DECIMAL, phys.FieldType_TYPE_DATE:
		if fd.Kind() != protoreflect.StringKind {
			return cf, fmt.Errorf("type %v requires string field, got %v", cf.Type, fd.Kind())
		}
	case phys.FieldType_TYPE_STRING:
		if fd.Kind() != protoreflect.StringKind {
			return cf, fmt.Errorf("type TYPE_STRING requires string field, got %v", fd.Kind())
		}
	case phys.FieldType_TYPE_UNSPECIFIED:
		// 缺省跟随 proto 字段类型：仅 string 显式落为 TYPE_STRING，其余保持 UNSPECIFIED
		if fd.Kind() == protoreflect.StringKind {
			cf.Type = phys.FieldType_TYPE_STRING
		}
	}

	// 规则 2/3：precision 归属
	if cf.HasPrecision {
		if cf.Type != phys.FieldType_TYPE_DECIMAL {
			return cf, fmt.Errorf("precision only allowed on TYPE_DECIMAL (field type %v)", cf.Type)
		}
		if cf.Precision < 0 {
			return cf, fmt.Errorf("negative precision %d", cf.Precision)
		}
	} else if cf.Type == phys.FieldType_TYPE_DECIMAL {
		return cf, fmt.Errorf("TYPE_DECIMAL requires precision")
	}

	// 规则 4/5：date_pattern 归属 + 宽度
	if cf.Type == phys.FieldType_TYPE_DATE {
		w, ok := dateWidth(cf.DatePattern)
		if !ok {
			return cf, fmt.Errorf("TYPE_DATE requires date_pattern")
		}
		if cf.Length != 0 && cf.Length != w {
			return cf, fmt.Errorf("date width mismatch: length %d, pattern width %d", cf.Length, w)
		}
	}

	// 格式规则（9/10/11）
	switch format {
	case phys.SourceFormat_FORMAT_FIXED:
		if cf.Length <= 0 {
			return cf, fmt.Errorf("fixed format requires length > 0 (got %d)", cf.Length)
		}
		if cf.Index < 0 {
			return cf, fmt.Errorf("negative index %d", cf.Index)
		}
	case phys.SourceFormat_FORMAT_DELIMITED:
		if lf.Offset != nil {
			return cf, fmt.Errorf("offset not allowed in delimited format")
		}
		if lf.Length != nil {
			return cf, fmt.Errorf("length not allowed in delimited format")
		}
		if cf.Index <= 0 {
			return cf, fmt.Errorf("delimited format requires index > 0")
		}
	}

	return cf, nil
}
