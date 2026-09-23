package physutil

// ── 渲染测试索引（对应定稿 §5.2）──
// 定长定位写：
//   TestRenderLine_ValidFixed                   多类型渲染（pad/符号/DECIMAL 逆转换）
//   TestRenderLine_BlankToFill                  空值/零值 → fill 占位（有值模型）
//   TestRenderLine_ExplicitOffsetGap            显式 offset 冗余区 → 空格 fill，行长 = max(offset+length)
//   TestRenderLine_SkipFieldPlaceholder         skip 字段 → fill 占位（列位不乱）
//   TestRenderLine_Truncate                     超长截断（B4）
//   TestRenderLine_BareScalar                   裸 scalar 值类型字段渲染
// 分隔符：
//   TestRenderDelimitedLine                     全列输出（无字段列空占位）+ SCALED 逆转换
//   TestRenderDelimitedLine_EmptyField          零值 → 空列（D12）
// 守卫：
//   TestRenderDelimitedLine_DescriptorMismatch  layout/DTO 类型不匹配 → 守卫报错
//   TestMapToStandard_DescriptorMismatch        mapping 源/目标类型不匹配 → 守卫报错

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/aif-go/agproto-spec/physutil/internal/testdata"
)

func renderMsg(t *testing.T, msg proto.Message) []byte {
	t.Helper()
	l, err := BuildCachedLayout(msg.ProtoReflect().Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	if l.Format().String() == "FORMAT_DELIMITED" {
		out, err := RenderDelimitedLine(l, msg)
		if err != nil {
			t.Fatalf("RenderDelimitedLine: %v", err)
		}
		return out
	}
	out, err := RenderLine(l, msg)
	if err != nil {
		t.Fatalf("RenderLine: %v", err)
	}
	return out
}

func setStr(m proto.Message, name string, v string) {
	ref := m.ProtoReflect()
	ref.Set(ref.Descriptor().Fields().ByName(protoreflect.Name(name)), protoreflect.ValueOfString(v))
}

func TestRenderLine_ValidFixed(t *testing.T) {
	m := (&testdata.ValidFixed{}).ProtoReflect().New().Interface()
	setStr(m, "name", "ZHANGSAN")
	setStr(m, "card", "6228480012345678")
	dto := m.ProtoReflect()
	dto.Set(dto.Descriptor().Fields().ByName("age"), protoreflect.ValueOfInt64(18))
	setStr(m, "amt", "123.45")
	setStr(m, "rate", "0.001234")
	setStr(m, "bizDt", "20260918")

	want := "ZHANGSAN  " + // name 10 PAD_RIGHT
		"0006228480012345678" + // card 19 PAD_LEFT fill 0
		"0018" + // age 4 PAD_LEFT fill 0
		"000000000012345" + // amt 15 SCALED → 12345
		"00001234" + // rate 8
		"20260918" // 8
	got := string(renderMsg(t, m))
	if got != want {
		t.Errorf("render:\n got %q\nwant %q", got, want)
	}
}

func TestRenderLine_BlankToFill(t *testing.T) {
	m := (&testdata.ValidFixed{}).ProtoReflect().New().Interface()
	setStr(m, "name", "ABC")
	// 其余字段零值/空白
	want := "ABC       " + // name
		"                   " + // card 空 → 空格 fill
		"0000" + // age 0 → PAD_LEFT 0 → "0000"
		"               " + // amt 空 → 空格
		"        " + // rate 空
		"        " // bizDt 空
	got := string(renderMsg(t, m))
	if got != want {
		t.Errorf("render:\n got %q\nwant %q", got, want)
	}
}

func TestRenderLine_ExplicitOffsetGap(t *testing.T) {
	m := (&testdata.ValidFixedOffset{}).ProtoReflect().New().Interface()
	setStr(m, "a", "AAAA")
	setStr(m, "b", "BBBB")
	out := renderMsg(t, m)
	if len(out) != 110 {
		t.Fatalf("line length = %d, want 110 (max offset+length)", len(out))
	}
	if string(out[0:10]) != "AAAA      " {
		t.Errorf("a region = %q", out[0:10])
	}
	// 10-100 冗余区 = 空格
	for i := 10; i < 100; i++ {
		if out[i] != ' ' {
			t.Fatalf("gap byte[%d] = %q, want space", i, out[i])
		}
	}
	if string(out[100:110]) != "BBBB      " {
		t.Errorf("b region = %q", out[100:110])
	}
}

func TestRenderLine_SkipFieldPlaceholder(t *testing.T) {
	m := (&testdata.ValidFixedSkip{}).ProtoReflect().New().Interface()
	setStr(m, "a", "AAA")
	setStr(m, "c", "CCC")
	out := renderMsg(t, m)
	want := "AAA  " + "     " + "CCC  " // b skip → 空格占位
	if string(out) != want {
		t.Errorf("render = %q, want %q", out, want)
	}
}

func TestRenderDelimitedLine(t *testing.T) {
	m := (&testdata.ValidDelimited{}).ProtoReflect().New().Interface()
	setStr(m, "a", "A")
	setStr(m, "b", "BB")
	dto := m.ProtoReflect()
	dto.Set(dto.Descriptor().Fields().ByName("c"), protoreflect.ValueOfInt64(7))
	setStr(m, "amt", "1.23")

	want := "A||BB|7|123" // 列2 无字段 → 空列；amt SCALED → 123
	got := string(renderMsg(t, m))
	if got != want {
		t.Errorf("render = %q, want %q", got, want)
	}
}

func TestRenderDelimitedLine_EmptyField(t *testing.T) {
	m := (&testdata.ValidDelimited{}).ProtoReflect().New().Interface()
	setStr(m, "a", "A")
	want := "A||||" // 其余空列
	got := string(renderMsg(t, m))
	if got != want {
		t.Errorf("render = %q, want %q", got, want)
	}
}

func TestRenderDelimitedLine_DescriptorMismatch(t *testing.T) {
	l, _ := BuildCachedLayout(md(t, &testdata.ValidDelimited{}))
	m := (&testdata.ValidFixed{}).ProtoReflect().New().Interface()
	if _, err := RenderDelimitedLine(l, m); err == nil {
		t.Fatal("expected descriptor mismatch error")
	}
	if _, err := RenderLine(l, m); err == nil {
		t.Fatal("expected descriptor mismatch error")
	}
}

func TestMapToStandard_DescriptorMismatch(t *testing.T) {
	srcL, _ := BuildCachedLayout(md(t, &testdata.MapSrc{}))
	dstL, _ := BuildCachedLayout(md(t, &testdata.MapDst{}))
	m, _ := BuildMapping(srcL, dstL)
	// 错误 dst 类型
	srcDTO := (&testdata.MapSrc{}).ProtoReflect().New().Interface()
	wrongDst := (&testdata.ValidFixed{}).ProtoReflect().New().Interface()
	if err := MapToStandard(m, srcDTO, wrongDst); err == nil {
		t.Fatal("expected descriptor mismatch error")
	}
}

func TestRenderLine_BareScalar(t *testing.T) {
	// 裸 scalar（非 optional）值类型字段渲染
	m := (&testdata.BareField{}).ProtoReflect().New().Interface()
	setStr(m, "a", "XY")
	dto := m.ProtoReflect()
	dto.Set(dto.Descriptor().Fields().ByName("b"), protoreflect.ValueOfInt64(9))
	want := "XY   " + "0009"
	got := string(renderMsg(t, m))
	if got != want {
		t.Errorf("render = %q, want %q", got, want)
	}
}

func TestRenderLine_Truncate(t *testing.T) {
	m := (&testdata.ValidFixed{}).ProtoReflect().New().Interface()
	setStr(m, "name", "TOOLONGFORNAME") // 11 字符 > 10 → 截断
	got := string(renderMsg(t, m))
	if got[0:10] != "TOOLONGFOR" {
		t.Errorf("truncate = %q", got[0:10])
	}
}
