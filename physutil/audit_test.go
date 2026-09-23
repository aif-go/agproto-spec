package physutil

// ── 审计回归测试（H1/H2/M1 修复 + L3/L4 补充）──
//   TestLayout_FormatSet            格式显式声明 vs 缺省（FormatSet）
//   TestLayout_HeaderMeta           header_lines/trailer_lines/line_separator/encoding
//   TestLayout_WithEncoding         编码覆盖（CLI/config 场景），原布局不变
//   TestLayout_FieldsImmutable      Fields() 返回副本（外部修改不污染布局）
//   TestLayout_FillMultiByte        多字节 fill → 构建报错
//   TestLayout_HugeLength           超限 length → 构建报错（防 int32 溢出 panic）
//   TestLayout_Decoder              Decoder() 导出可用（GBK 解码）
//   TestCheckFFFD                   CheckFFFD 导出
//   TestRenderLine_ExplicitZero     EXPLICIT 显式零 "0.00" 渲染（L4）

import (
	"bytes"
	"testing"

	"github.com/aif-go/agproto-spec/physutil/internal/testdata"
)

func TestLayout_FormatSet(t *testing.T) {
	l, err := BuildCachedLayout(md(t, &testdata.ValidFixed{})) // 显式 format: FIXED
	if err != nil {
		t.Fatal(err)
	}
	if !l.FormatSet() {
		t.Error("ValidFixed: FormatSet() should be true (显式声明)")
	}
	l2, err := BuildCachedLayout(md(t, &testdata.NoFileOpt{})) // 无 file 注解
	if err != nil {
		t.Fatal(err)
	}
	if l2.FormatSet() {
		t.Error("NoFileOpt: FormatSet() should be false (缺省)")
	}
	if l2.Format() != FormatFixed {
		t.Errorf("NoFileOpt format = %v, want FIXED 缺省", l2.Format())
	}
}

func TestLayout_HeaderMeta(t *testing.T) {
	l, err := BuildCachedLayout(md(t, &testdata.WithHeader{}))
	if err != nil {
		t.Fatal(err)
	}
	if l.HeaderLines() != 1 || l.TrailerLines() != 2 {
		t.Errorf("header/trailer = %d/%d, want 1/2", l.HeaderLines(), l.TrailerLines())
	}
	if l.LineSeparator() != "\r\n" {
		t.Errorf("lineSeparator = %q", l.LineSeparator())
	}
	if l.Encoding() != "GBK" {
		t.Errorf("encoding = %q", l.Encoding())
	}
}

func TestLayout_WithEncoding(t *testing.T) {
	l, err := BuildCachedLayout(md(t, &testdata.ValidFixed{}))
	if err != nil {
		t.Fatal(err)
	}
	// 覆盖为 GBK：解析 GBK 行
	gbkL, err := l.WithEncoding("GBK")
	if err != nil {
		t.Fatal(err)
	}
	if gbkL.Encoding() != "GBK" {
		t.Errorf("encoding = %q", gbkL.Encoding())
	}
	// 原布局不变
	if l.Encoding() != "UTF-8" {
		t.Errorf("original encoding changed: %q", l.Encoding())
	}
	// 空名/同名 → 原实例
	if same, _ := l.WithEncoding(""); same != l {
		t.Error("WithEncoding(\"\") should return same instance")
	}
	// 未知 → error
	if _, err := l.WithEncoding("NOSUCH"); err == nil {
		t.Error("expected unsupported encoding error")
	}
}

func TestLayout_FieldsImmutable(t *testing.T) {
	l, err := BuildCachedLayout(md(t, &testdata.ValidFixed{}))
	if err != nil {
		t.Fatal(err)
	}
	fs := l.Fields()
	fs[0].Length = 999 // 外部修改副本
	if got := l.Fields()[0].Length; got == 999 {
		t.Error("Fields() 返回共享切片，外部修改污染布局")
	}
}

func TestLayout_FillMultiByte(t *testing.T) {
	_, err := BuildCachedLayout(md(t, &testdata.FillMultiByte{}))
	if err == nil {
		t.Fatal("expected fill single-byte error")
	}
}

func TestLayout_HugeLength(t *testing.T) {
	_, err := BuildCachedLayout(md(t, &testdata.HugeLength{}))
	if err == nil {
		t.Fatal("expected length limit error (防 int32 溢出 panic)")
	}
}

func TestLayout_Decoder(t *testing.T) {
	l, err := BuildCachedLayout(md(t, &testdata.EncFixed{})) // GBK
	if err != nil {
		t.Fatal(err)
	}
	dec, err := l.Decoder().Bytes(append(append([]byte{}, gbkNameBytes...), bytes.Repeat([]byte{' '}, 16)...))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(bytes.TrimRight(dec, " ")); got != "张三" {
		t.Errorf("decoder = %q", got)
	}
}

func TestCheckFFFD(t *testing.T) {
	if err := CheckFFFD([]byte{0xEF, 0xBF, 0xBD}); err == nil {
		t.Error("U+FFFD should error")
	}
	if err := CheckFFFD([]byte("正常内容")); err != nil {
		t.Errorf("valid UTF-8 should pass: %v", err)
	}
}

func TestRenderLine_ExplicitZero(t *testing.T) {
	// L4：EXPLICIT 显式零 "0.00"（区别于空值 ""）→ 渲染 "00000.00"（8 字节）
	l, err := BuildCachedLayout(md(t, &testdata.DecimalExplicitOnly{}))
	if err != nil {
		t.Fatal(err)
	}
	m := (&testdata.DecimalExplicitOnly{}).ProtoReflect().New().Interface()
	setStr(m, "amt", "0.00")
	out, err := RenderLine(l, m)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "00000.00" {
		t.Errorf("render = %q, want 00000.00（显式零）", out)
	}
}
