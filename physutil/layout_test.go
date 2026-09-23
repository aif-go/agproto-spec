package physutil

// ── 布局/校验测试索引（对应定稿 §3.5 校验规则）──
// 合法构建：
//   TestBuildCachedLayout_ValidFixed            index 推算 offset + 类型元数据（DECIMAL/DATE）
//   TestBuildCachedLayout_ValidFixedOffset      显式 offset 覆盖（冗余区）
//   TestBuildCachedLayout_ValidDelimited        合法分隔符 + delimiter
//   TestBuildCachedLayout_ByName                字段名索引 O(1) 查询
//   TestBuildCachedLayout_DelimiterVariants     delimiter 单字符 / tab / 多字符 "||"
// 非法校验（§3.5 表驱动）：
//   TestBuildCachedLayout_Validation            overlap / dup index / 缺 length / 分隔符带 offset /
//                                              DECIMAL 缺 precision / STRING 带 precision / DATE 缺 pattern /
//                                              DATE 宽度不符 / bool 禁 / int64 类型不符 / 零 length /
//                                              零 index / 负 offset / 空 delimiter / enum kind

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/aif-go/agproto-spec/phys"
	"github.com/aif-go/agproto-spec/physutil/internal/testdata"
)

func md(t *testing.T, m protoreflect.ProtoMessage) protoreflect.MessageDescriptor {
	t.Helper()
	return m.ProtoReflect().Descriptor()
}

// ── 合法消息：构建成功 + 布局正确 ──

func TestBuildCachedLayout_ValidFixed(t *testing.T) {
	l, err := BuildCachedLayout(md(t, &testdata.ValidFixed{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if l.Format() != phys.SourceFormat_FORMAT_FIXED {
		t.Errorf("format = %v, want FIXED", l.Format())
	}
	// index 推算 offset：name 0-10, card 10-29, age 29-33, amt 33-48, rate 48-56, bizDt 56-64
	want := []struct {
		name   string
		offset int32
		length int32
	}{
		{"name", 0, 10}, {"card", 10, 19}, {"age", 29, 4},
		{"amt", 33, 15}, {"rate", 48, 8}, {"bizDt", 56, 8},
	}
	if len(l.Fields()) != len(want) {
		t.Fatalf("fields = %d, want %d", len(l.Fields()), len(want))
	}
	for i, w := range want {
		f := l.Fields()[i]
		if f.FieldName != w.name || f.Offset != w.offset || f.Length != w.length {
			t.Errorf("field[%d] = %s@%d len %d, want %s@%d len %d",
				i, f.FieldName, f.Offset, f.Length, w.name, w.offset, w.length)
		}
	}
	// 类型元数据
	if l.Fields()[3].Type != phys.FieldType_TYPE_DECIMAL || !l.Fields()[3].HasPrecision || l.Fields()[3].Precision != 2 {
		t.Errorf("amt metadata wrong: %+v", l.Fields()[3])
	}
	if l.Fields()[5].Type != phys.FieldType_TYPE_DATE || l.Fields()[5].DatePattern != phys.DatePattern_DATE_PATTERN_YYYYMMDD {
		t.Errorf("bizDt metadata wrong: %+v", l.Fields()[5])
	}
}

func TestBuildCachedLayout_ValidFixedOffset(t *testing.T) {
	l, err := BuildCachedLayout(md(t, &testdata.ValidFixedOffset{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fs := l.Fields()
	if fs[0].Offset != 0 || fs[1].Offset != 100 {
		t.Errorf("offsets = %d,%d want 0,100 (explicit override)", fs[0].Offset, fs[1].Offset)
	}
}

func TestBuildCachedLayout_ValidDelimited(t *testing.T) {
	l, err := BuildCachedLayout(md(t, &testdata.ValidDelimited{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if l.Format() != phys.SourceFormat_FORMAT_DELIMITED || l.Delimiter() != "|" {
		t.Errorf("format/delimiter = %v/%q", l.Format(), l.Delimiter())
	}
	if l.Fields()[1].Index != 3 {
		t.Errorf("field[1] index = %d, want 3", l.Fields()[1].Index)
	}
}

func TestBuildCachedLayout_ByName(t *testing.T) {
	l, err := BuildCachedLayout(md(t, &testdata.ValidFixed{}))
	if err != nil {
		t.Fatal(err)
	}
	f, ok := l.Field("amt")
	if !ok || f.Offset != 33 {
		t.Errorf("Field(amt) = %+v, ok=%v", f, ok)
	}
	if _, ok := l.Field("nope"); ok {
		t.Error("Field(nope) should not exist")
	}
}

func TestBuildCachedLayout_DelimiterVariants(t *testing.T) {
	for _, tt := range []struct {
		name string
		msg  protoreflect.ProtoMessage
		del  string
	}{
		{"single", &testdata.ValidDelimited{}, "|"},
		{"tab", &testdata.ValidDelimitedTab{}, "\t"},
		{"multi-char", &testdata.ValidDelimitedMulti{}, "||"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			l, err := BuildCachedLayout(md(t, tt.msg))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if l.Delimiter() != tt.del {
				t.Errorf("delimiter = %q, want %q", l.Delimiter(), tt.del)
			}
		})
	}
}

// ── 非法消息：每条触发一条校验规则 ──

func TestBuildCachedLayout_Validation(t *testing.T) {
	tests := []struct {
		name    string
		msg     protoreflect.ProtoMessage
		wantErr string
	}{
		{"overlap", &testdata.Overlap{}, "overlap"},
		{"dup index", &testdata.DupIndex{}, "index"},
		{"fixed no length", &testdata.NoLength{}, "length"},
		{"delimited with offset", &testdata.DelimitedWithOffset{}, "offset"},
		{"decimal missing precision", &testdata.DecimalNoPrecision{}, "precision"},
		{"string with precision", &testdata.StringWithPrecision{}, "precision"},
		{"date missing pattern", &testdata.DateNoPattern{}, "date"},
		{"date width mismatch", &testdata.DateWidthMismatch{}, "width"},
		{"bool forbidden", &testdata.BoolField{}, "bool"},
		{"int64 type mismatch", &testdata.IntTypeMismatch{}, "type"},
		{"zero length", &testdata.ZeroLength{}, "length"},
		{"zero index", &testdata.ZeroIndex{}, "index"},
		{"negative offset", &testdata.NegativeOffset{}, "offset"},
		{"empty delimiter", &testdata.EmptyDelimiter{}, "delimiter"},
		{"enum kind", &testdata.EnumField{}, "unsupported kind"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildCachedLayout(md(t, tt.msg))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want containing %q", err.Error(), tt.wantErr)
			}
		})
	}
}
