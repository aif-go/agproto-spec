package physutil

// ── proto2 业务文件兼容性（跨语法使用 proto3 phys 注解）──
//   TestProto2_BuildAndParse   构建 + 解析（proto2 optional 字段）
//   TestProto2_BlankZero       有值模型：proto2 空白 → 零值（Set 非 nil）
//   TestProto2_DescriptorMismatch 描述符守卫（跨包类型）

import (
	"testing"

	"github.com/aif-go/agproto-spec/physutil/internal/testdata"
)

func TestProto2_BuildAndParse(t *testing.T) {
	l, err := BuildCachedLayout(md(t, &testdata.Proto2Msg{}))
	if err != nil {
		t.Fatalf("proto2 构建失败: %v", err)
	}
	// 布局：name 10 + qty 15(PAD_LEFT fill 0) + date 8 = 33 字节
	line := "ZHANGSAN  " + "000000000000042" + "20260918"
	dest := (&testdata.Proto2Msg{}).ProtoReflect().New().Interface()
	if err := ParseLine(l, dest, []byte(line)); err != nil {
		t.Fatalf("proto2 解析失败: %v", err)
	}
	fields := dest.ProtoReflect().Descriptor().Fields()
	if got := dest.ProtoReflect().Get(fields.ByName("name")).String(); got != "ZHANGSAN" {
		t.Errorf("name = %q", got)
	}
	if got := dest.ProtoReflect().Get(fields.ByName("qty")).Int(); got != 42 {
		t.Errorf("qty = %d", got)
	}
	if got := dest.ProtoReflect().Get(fields.ByName("date")).String(); got != "20260918" {
		t.Errorf("date = %q", got)
	}
}

func TestProto2_BlankZero(t *testing.T) {
	l, err := BuildCachedLayout(md(t, &testdata.Proto2Msg{}))
	if err != nil {
		t.Fatal(err)
	}
	line := "          " + "000000000000000" + "        "
	dest := (&testdata.Proto2Msg{}).ProtoReflect().New().Interface()
	if err := ParseLine(l, dest, []byte(line)); err != nil {
		t.Fatalf("proto2 空白解析失败: %v", err)
	}
	ref := dest.ProtoReflect()
	fields := ref.Descriptor().Fields()
	// 有值模型：proto2 optional 字段空白 → Set 零值（指针非 nil）
	if got := ref.Get(fields.ByName("name")).String(); got != "" {
		t.Errorf("blank name = %q", got)
	}
	if !ref.Has(fields.ByName("name")) {
		t.Error("proto2 blank field should be SET (有值模型, 非 nil)")
	}
	if got := ref.Get(fields.ByName("qty")).Int(); got != 0 {
		t.Errorf("blank qty = %d", got)
	}
}

func TestProto2_DescriptorMismatch(t *testing.T) {
	l, err := BuildCachedLayout(md(t, &testdata.Proto2Msg{}))
	if err != nil {
		t.Fatal(err)
	}
	dest := (&testdata.ValidFixed{}).ProtoReflect().New().Interface() // 跨包类型
	if err := ParseLine(l, dest, []byte("123456789012345678901234567890123")); err == nil {
		t.Fatal("expected descriptor mismatch error")
	}
}
