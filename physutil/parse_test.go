package physutil

// ── 解析测试索引（按场景定位用例）──
// 定长基础：
//   TestParseLine_ValidFixed                   多类型行解析（string/int64/DECIMAL p2/p6/DATE，index 推算 offset）
//   TestParseLine_BlankToZeroValue             有值模型：空白 → 零值 ""/0（非 nil）
//   TestParseLine_DecimalEdges                 SCALED 边界：小值/大值/全 0 空白/非数字报错
//   TestParseLine_ExplicitDecimal              EXPLICIT 模式："01234.56" → "1234.56"
//   TestParseLine_ExplicitDecimal_LeadingZeroValue  "0.56" 前导 0 不被 trim 破坏（BUG-1 回归）
//   TestParseLine_Int32                        int32 字段解析 + 空白零值（不 panic）
//   TestParseLine_BareScalar                   裸 scalar（非 optional）解析 + 有值模型
//   TestParseLine_ShortLine                    行短 → error（B1）
//   TestParseLine_DescriptorMismatch           layout/DTO 类型不匹配 → 守卫报错（防 panic）
// 分隔符：
//   TestParseDelimitedLine_Valid               列定位（跳过无字段列）+ DECIMAL
//   TestParseDelimitedLine_MultiChar           多字符 delimiter "||"
//   TestParseDelimitedLine_Tab                 tab delimiter
//   TestParseDelimitedLine_ColumnCountTooFew   列数不足 → error
//   TestParseDelimitedLine_EmptyField          空列 → 零值

import (
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/aif-go/agproto-spec/physutil/internal/testdata"
)

func parseFixed(t *testing.T, msg proto.Message, line string) proto.Message {
	t.Helper()
	l, err := BuildCachedLayout(msg.ProtoReflect().Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	dest := msg.ProtoReflect().New().Interface()
	if err := ParseLine(l, dest, []byte(line)); err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	return dest
}

func TestParseLine_ValidFixed(t *testing.T) {
	// 布局：name 0-10(PAD_RIGHT 空格) / card 10-29(PAD_LEFT fill 0) / age 29-33 / amt 33-48(p2 SCALED) / rate 48-56(p6) / bizDt 56-64
	line := "ZHANGSAN  " + // 10
		"0006228480012345678" + // 19
		"0018" + // 4
		"000000000012345" + // 15 → 123.45
		"00001234" + // 8 → 0.001234
		"20260918" // 8
	dto := parseFixed(t, &testdata.ValidFixed{}, line)
	ref := dto.ProtoReflect()
	fields := ref.Descriptor().Fields()
	if got := ref.Get(fields.ByName("name")).String(); got != "ZHANGSAN" {
		t.Errorf("name = %q", got)
	}
	if got := ref.Get(fields.ByName("card")).String(); got != "6228480012345678" {
		t.Errorf("card = %q (left-trim 0)", got)
	}
	if got := ref.Get(fields.ByName("age")).Int(); got != 18 {
		t.Errorf("age = %d", got)
	}
	if got := ref.Get(fields.ByName("amt")).String(); got != "123.45" {
		t.Errorf("amt = %q, want 123.45", got)
	}
	if got := ref.Get(fields.ByName("rate")).String(); got != "0.001234" {
		t.Errorf("rate = %q, want 0.001234", got)
	}
	if got := ref.Get(fields.ByName("bizDt")).String(); got != "20260918" {
		t.Errorf("bizDt = %q", got)
	}
}

func TestParseLine_BlankToZeroValue(t *testing.T) {
	// name 全空格 → ""；age 全 0 → 0（有值模型：非 nil，零值）
	line := "          " + // 10 空格
		"0000000000000000000" + // 19
		"0000" + // 4
		"000000000000000" + // 15
		"00000000" + // 8
		"        " // 8 空格
	dto := parseFixed(t, &testdata.ValidFixed{}, line)
	ref := dto.ProtoReflect()
	fdName := ref.Descriptor().Fields().ByName("name")
	if got := ref.Get(fdName).String(); got != "" {
		t.Errorf("blank name = %q, want empty", got)
	}
	if !ref.Has(fdName) {
		t.Error("blank name should be SET (有值模型, 非 nil)")
	}
	if got := ref.Get(ref.Descriptor().Fields().ByName("age")).Int(); got != 0 {
		t.Errorf("blank age = %d, want 0", got)
	}
	if got := ref.Get(ref.Descriptor().Fields().ByName("amt")).String(); got != "" {
		t.Errorf("blank amt = %q, want empty", got)
	}
}

func TestParseLine_DecimalEdges(t *testing.T) {
	tests := []struct {
		name    string
		line    string // 15 字节 amt 字段（SCALED p2）
		wantAmt string
		wantErr bool
	}{
		{"scaled small", "000000000000123", "1.23", false},
		{"scaled big", "012345678901234", "123456789012.34", false},
		{"all zeros blank", "000000000000000", "", false}, // 全 fill → 零值 ""（有值模型）
		{"non-digit", "12A450000000000", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, err := BuildCachedLayout(md(t, &testdata.DecimalOnly{}))
			if err != nil {
				t.Fatal(err)
			}
			dest := (&testdata.DecimalOnly{}).ProtoReflect().New().Interface()
			err = ParseLine(l, dest, []byte(tt.line))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := dest.ProtoReflect().Get(dest.ProtoReflect().Descriptor().Fields().ByName("amt")).String()
			if got != tt.wantAmt {
				t.Errorf("amt = %q, want %q", got, tt.wantAmt)
			}
		})
	}
}

func TestParseLine_ExplicitDecimal(t *testing.T) {
	l, err := BuildCachedLayout(md(t, &testdata.DecimalExplicitOnly{}))
	if err != nil {
		t.Fatal(err)
	}
	dest := (&testdata.DecimalExplicitOnly{}).ProtoReflect().New().Interface()
	if err := ParseLine(l, dest, []byte("01234.56")); err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	got := dest.ProtoReflect().Get(dest.ProtoReflect().Descriptor().Fields().ByName("amt")).String()
	if got != "1234.56" {
		t.Errorf("amt = %q, want 1234.56", got)
	}
}

func TestParseLine_ExplicitDecimal_LeadingZeroValue(t *testing.T) {
	// "0.56" 左补 0 后 "00000.56"——前导 0 不能被 trim 破坏（BUG-1 回归）
	l, err := BuildCachedLayout(md(t, &testdata.DecimalExplicitOnly{}))
	if err != nil {
		t.Fatal(err)
	}
	dest := (&testdata.DecimalExplicitOnly{}).ProtoReflect().New().Interface()
	if err := ParseLine(l, dest, []byte("00000.56")); err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	got := dest.ProtoReflect().Get(dest.ProtoReflect().Descriptor().Fields().ByName("amt")).String()
	if got != "0.56" {
		t.Errorf("amt = %q, want 0.56", got)
	}
}

func TestParseLine_Int32(t *testing.T) {
	l, err := BuildCachedLayout(md(t, &testdata.Int32Field{}))
	if err != nil {
		t.Fatal(err)
	}
	dest := (&testdata.Int32Field{}).ProtoReflect().New().Interface()
	if err := ParseLine(l, dest, []byte("00042")); err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	if got := dest.ProtoReflect().Get(dest.ProtoReflect().Descriptor().Fields().ByName("n")).Int(); got != 42 {
		t.Errorf("n = %d, want 42", got)
	}
	// 空白 → 零值 0（不 panic，有值模型）
	dest2 := (&testdata.Int32Field{}).ProtoReflect().New().Interface()
	if err := ParseLine(l, dest2, []byte("00000")); err != nil {
		t.Fatalf("ParseLine blank: %v", err)
	}
	if got := dest2.ProtoReflect().Get(dest2.ProtoReflect().Descriptor().Fields().ByName("n")).Int(); got != 0 {
		t.Errorf("blank n = %d, want 0", got)
	}
}

func TestParseLine_DescriptorMismatch(t *testing.T) {
	l, err := BuildCachedLayout(md(t, &testdata.ValidFixed{}))
	if err != nil {
		t.Fatal(err)
	}
	// 用另一个消息类型 → 守卫报错而非 panic
	dest := (&testdata.DecimalOnly{}).ProtoReflect().New().Interface()
	err = ParseLine(l, dest, []byte("012345678901234"))
	if err == nil {
		t.Fatal("expected descriptor mismatch error")
	}
}

func TestParseLine_BareScalar(t *testing.T) {
	// 裸 scalar（非 optional）：无 presence 值类型字段，有值模型照常工作
	l, err := BuildCachedLayout(md(t, &testdata.BareField{}))
	if err != nil {
		t.Fatal(err)
	}
	// 正常值
	dest := (&testdata.BareField{}).ProtoReflect().New().Interface()
	if err := ParseLine(l, dest, []byte("AB   0007")); err != nil { // a 5 字节 + b 4 字节 = 9
		t.Fatalf("ParseLine: %v", err)
	}
	fields := dest.ProtoReflect().Descriptor().Fields()
	if got := dest.ProtoReflect().Get(fields.ByName("a")).String(); got != "AB" {
		t.Errorf("a = %q", got)
	}
	if got := dest.ProtoReflect().Get(fields.ByName("b")).Int(); got != 7 {
		t.Errorf("b = %d", got)
	}
	// 空白 → 零值（不 panic，值类型字段 Set 正常）
	dest2 := (&testdata.BareField{}).ProtoReflect().New().Interface()
	if err := ParseLine(l, dest2, []byte("     0000")); err != nil {
		t.Fatalf("ParseLine blank: %v", err)
	}
	if got := dest2.ProtoReflect().Get(fields.ByName("a")).String(); got != "" {
		t.Errorf("blank a = %q", got)
	}
	if got := dest2.ProtoReflect().Get(fields.ByName("b")).Int(); got != 0 {
		t.Errorf("blank b = %d", got)
	}
}

func TestParseLine_ShortLine(t *testing.T) {
	dto := &testdata.ValidFixed{}
	l, err := BuildCachedLayout(dto.ProtoReflect().Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	dest := dto.ProtoReflect().New().Interface()
	if err := ParseLine(l, dest, []byte("short")); err == nil {
		t.Fatal("expected error for short line")
	}
}

func TestParseDelimitedLine_Valid(t *testing.T) {
	dto := &testdata.ValidDelimited{}
	l, err := BuildCachedLayout(dto.ProtoReflect().Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	dest := dto.ProtoReflect().New().Interface()
	// 列：1=a, 2=忽略, 3=b, 4=c, 5=amt
	if err := ParseDelimitedLine(l, dest, []byte("A|x|BB|7|000123")); err != nil {
		t.Fatalf("ParseDelimitedLine: %v", err)
	}
	ref := dest.ProtoReflect()
	fields := ref.Descriptor().Fields()
	if got := ref.Get(fields.ByName("a")).String(); got != "A" {
		t.Errorf("a = %q", got)
	}
	if got := ref.Get(fields.ByName("b")).String(); got != "BB" {
		t.Errorf("b = %q", got)
	}
	if got := ref.Get(fields.ByName("c")).Int(); got != 7 {
		t.Errorf("c = %d", got)
	}
	if got := ref.Get(fields.ByName("amt")).String(); got != "1.23" {
		t.Errorf("amt = %q, want 1.23", got)
	}
}

func TestParseDelimitedLine_MultiChar(t *testing.T) {
	dto := &testdata.ValidDelimitedMulti{}
	l, err := BuildCachedLayout(dto.ProtoReflect().Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	dest := dto.ProtoReflect().New().Interface()
	if err := ParseDelimitedLine(l, dest, []byte("A||B")); err != nil {
		t.Fatalf("ParseDelimitedLine: %v", err)
	}
	fields := dest.ProtoReflect().Descriptor().Fields()
	if got := dest.ProtoReflect().Get(fields.ByName("a")).String(); got != "A" {
		t.Errorf("a = %q", got)
	}
	if got := dest.ProtoReflect().Get(fields.ByName("b")).String(); got != "B" {
		t.Errorf("b = %q", got)
	}
}

func TestParseDelimitedLine_Tab(t *testing.T) {
	dto := &testdata.ValidDelimitedTab{}
	l, err := BuildCachedLayout(dto.ProtoReflect().Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	dest := dto.ProtoReflect().New().Interface()
	if err := ParseDelimitedLine(l, dest, []byte("A\tB")); err != nil {
		t.Fatalf("ParseDelimitedLine: %v", err)
	}
	fields := dest.ProtoReflect().Descriptor().Fields()
	if got := dest.ProtoReflect().Get(fields.ByName("a")).String(); got != "A" {
		t.Errorf("a = %q", got)
	}
	if got := dest.ProtoReflect().Get(fields.ByName("b")).String(); got != "B" {
		t.Errorf("b = %q", got)
	}
}

func TestParseDelimitedLine_ColumnCountTooFew(t *testing.T) {
	dto := &testdata.ValidDelimited{}
	l, err := BuildCachedLayout(dto.ProtoReflect().Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	dest := dto.ProtoReflect().New().Interface()
	if err := ParseDelimitedLine(l, dest, []byte("A|x|BB")); err == nil {
		t.Fatal("expected error: index 4 exceeds column count 3")
	}
}

func TestParseDelimitedLine_EmptyField(t *testing.T) {
	dto := &testdata.ValidDelimited{}
	l, err := BuildCachedLayout(dto.ProtoReflect().Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	dest := dto.ProtoReflect().New().Interface()
	if err := ParseDelimitedLine(l, dest, []byte("A||BB|7|000123")); err != nil {
		t.Fatalf("ParseDelimitedLine: %v", err)
	}
	if got := dest.ProtoReflect().Get(dest.ProtoReflect().Descriptor().Fields().ByName("b")).String(); got != "BB" {
		t.Errorf("b = %q", got)
	}
}
