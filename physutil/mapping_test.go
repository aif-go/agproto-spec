package physutil

// ── 映射测试索引（对应定稿 §5.3）──
//   TestBuildMapping_Valid                    target 注解建映射（skip 不参与）
//   TestMapToStandard_EndToEnd                全链路：源行 → Parse → Map → Render 标准行
//   TestBuildMapping_ScaleUp                  DECIMAL 升精度：无损补零（"123.45" → "123.4500"）
//   TestBuildMapping_LossyPrecision           DECIMAL 降精度：构建报错（有损，业务决策）
//   TestBuildMapping_TargetNotFound           target 目标字段不存在 → 构建报错

import (
	"strings"
	"testing"

	"github.com/aif-go/agproto-spec/physutil/internal/testdata"
)

func TestBuildMapping_Valid(t *testing.T) {
	srcL, err := BuildCachedLayout(md(t, &testdata.MapSrc{}))
	if err != nil {
		t.Fatal(err)
	}
	dstL, err := BuildCachedLayout(md(t, &testdata.MapDst{}))
	if err != nil {
		t.Fatal(err)
	}
	m, err := BuildMapping(srcL, dstL)
	if err != nil {
		t.Fatalf("BuildMapping: %v", err)
	}
	// target: code→code, amt→amt, qty→qty；junk skip 不参与
	if len(m.Entries()) != 3 {
		t.Fatalf("entries = %d, want 3 (junk skipped)", len(m.Entries()))
	}
}

func TestMapToStandard_EndToEnd(t *testing.T) {
	srcL, _ := BuildCachedLayout(md(t, &testdata.MapSrc{}))
	dstL, _ := BuildCachedLayout(md(t, &testdata.MapDst{}))
	m, err := BuildMapping(srcL, dstL)
	if err != nil {
		t.Fatal(err)
	}

	// 源行：code 5 + amt 15 + junk 5 + qty 4 = 29
	srcLine := "AB001" + "000000000012345" + "XXXXX" + "0007"
	srcDTO := (&testdata.MapSrc{}).ProtoReflect().New().Interface()
	if err := ParseLine(srcL, srcDTO, []byte(srcLine)); err != nil {
		t.Fatal(err)
	}

	dstDTO := (&testdata.MapDst{}).ProtoReflect().New().Interface()
	if err := MapToStandard(m, srcDTO, dstDTO); err != nil {
		t.Fatalf("MapToStandard: %v", err)
	}
	ref := dstDTO.ProtoReflect()
	fields := ref.Descriptor().Fields()
	if got := ref.Get(fields.ByName("code")).String(); got != "AB001" {
		t.Errorf("code = %q", got)
	}
	if got := ref.Get(fields.ByName("amt")).String(); got != "123.45" {
		t.Errorf("amt = %q (DECIMAL 同精度拷贝)", got)
	}
	if got := ref.Get(fields.ByName("qty")).String(); got != "7" {
		t.Errorf("qty = %q (int64 → string)", got)
	}

	// 渲染标准行：code 5 + amt 15 + qty 4
	out, err := RenderLine(dstL, dstDTO)
	if err != nil {
		t.Fatal(err)
	}
	want := "AB001" + "000000000012345" + "0007"
	if string(out) != want {
		t.Errorf("render = %q, want %q", out, want)
	}
}

func TestBuildMapping_ScaleUp(t *testing.T) {
	srcL, _ := BuildCachedLayout(md(t, &testdata.MapSrc{}))
	dstL, err := BuildCachedLayout(md(t, &testdata.MapDstScale4{}))
	if err != nil {
		t.Fatal(err)
	}
	m, err := BuildMapping(srcL, dstL)
	if err != nil {
		t.Fatalf("scale-up should be allowed: %v", err)
	}
	// 只映射 amt（MapDstScale4 只有 amt 字段）
	srcDTO := (&testdata.MapSrc{}).ProtoReflect().New().Interface()
	srcLine := "AB001" + "000000000012345" + "XXXXX" + "0007"
	if err := ParseLine(srcL, srcDTO, []byte(srcLine)); err != nil {
		t.Fatal(err)
	}
	dstDTO := (&testdata.MapDstScale4{}).ProtoReflect().New().Interface()
	if err := MapToStandard(m, srcDTO, dstDTO); err != nil {
		t.Fatal(err)
	}
	got := dstDTO.ProtoReflect().Get(dstDTO.ProtoReflect().Descriptor().Fields().ByName("amt")).String()
	if got != "123.4500" {
		t.Errorf("amt = %q, want 123.4500 (无损补零)", got)
	}
}

func TestBuildMapping_LossyPrecision(t *testing.T) {
	srcL, _ := BuildCachedLayout(md(t, &testdata.MapSrc{}))
	dstL, _ := BuildCachedLayout(md(t, &testdata.MapDstLossy{}))
	_, err := BuildMapping(srcL, dstL)
	if err == nil || !strings.Contains(err.Error(), "lossy") {
		t.Fatalf("expected lossy precision error, got %v", err)
	}
}

func TestBuildMapping_TargetNotFound(t *testing.T) {
	srcL, _ := BuildCachedLayout(md(t, &testdata.MapSrc{}))
	dstL, _ := BuildCachedLayout(md(t, &testdata.ValidFixed{})) // 无 code/amt/qty 字段
	_, err := BuildMapping(srcL, dstL)
	if err == nil || !strings.Contains(err.Error(), "target") {
		t.Fatalf("expected target-not-found error, got %v", err)
	}
}
