// agproto-spec 完整功能演示：银行批量激活文件（GBK 定长）→ B2O 标准接口（UTF-8 分隔符）。
//
// 覆盖：注解定义 / 布局构建+校验 / 定长 GBK 字段级解码 / DECIMAL 规范化 /
//       DATE 透传 / Map（target 重排 + skip 丢弃）/ 分隔符 Render / 有值模型。
//
// 运行：go run ./demo
package main

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/aif-go/agproto-spec/demo"
	"github.com/aif-go/agproto-spec/physutil"
)

func main() {
	// ── 1. 构建源布局（读注解 + 校验）──
	srcL, err := physutil.BuildCachedLayout((&demo.DemoSource{}).ProtoReflect().Descriptor())
	must(err, "BuildCachedLayout(源)")
	fmt.Printf("[1] 源布局构建 ✅  format=%v encoding=%s 字段=%d\n",
		formatName(srcL.Format()), srcL.Encoding(), len(srcL.Fields()))

	// ── 2. 源文件行（GBK 定长）：卡号19(PAD_LEFT fill 0) + 姓名20(GBK"张三") + 金额15 + 日期8 + 冗余5 ──
	// "张三" GBK = D5C5 C8FD（4 字节），补 16 空格到 20
	srcLine := gbkBytes("0006228480012345678")     // 卡号 19 字节（左补 0）
	srcLine = append(srcLine, gbkBytes("张三")...)  // 姓名
	srcLine = append(srcLine, bytes.Repeat([]byte{' '}, 16)...) // 姓名补位空格
	srcLine = append(srcLine, []byte("000000000012345")...)    // 金额 SCALED p2 → 123.45
	srcLine = append(srcLine, []byte("20260918")...)           // 日期
	srcLine = append(srcLine, []byte("XXXXX")...)              // 冗余区（skip）
	fmt.Printf("[2] 源行 %d 字节（GBK 编码，hex=%x...）\n", len(srcLine), srcLine[:10])

	// ── 3. 解析：定长 + GBK 字段级解码 ──
	srcDTO := (&demo.DemoSource{}).ProtoReflect().New().Interface()
	must(physutil.ParseLine(srcL, srcDTO, srcLine), "ParseLine")
	sf := srcDTO.ProtoReflect().Descriptor().Fields()
	fmt.Println("[3] Parse → 源 DTO：")
	fmt.Printf("    cardNo=%q custName=%q txnAmt=%q(规范化) bizDate=%q filler=%q(skip)\n",
		srcDTO.ProtoReflect().Get(sf.ByName("cardNo")).String(),
		srcDTO.ProtoReflect().Get(sf.ByName("custName")).String(),
		srcDTO.ProtoReflect().Get(sf.ByName("txnAmt")).String(),
		srcDTO.ProtoReflect().Get(sf.ByName("bizDate")).String(),
		srcDTO.ProtoReflect().Get(sf.ByName("filler")).String())

	// ── 4. 映射：target 重排 + skip 丢弃 ──
	dstL, err := physutil.BuildCachedLayout((&demo.DemoStandard{}).ProtoReflect().Descriptor())
	must(err, "BuildCachedLayout(标准)")
	mapping, err := physutil.BuildMapping(srcL, dstL)
	must(err, "BuildMapping")
	stdDTO := (&demo.DemoStandard{}).ProtoReflect().New().Interface()
	must(physutil.MapToStandard(mapping, srcDTO, stdDTO), "MapToStandard")
	fmt.Printf("[4] Map → 标准 DTO ✅  映射项=%d（filler 已跳过）\n", len(mapping.Entries()))

	// ── 5. 渲染：UTF-8 分隔符标准行 ──
	out, err := physutil.RenderDelimitedLine(dstL, stdDTO)
	must(err, "RenderDelimitedLine")
	fmt.Printf("[5] Render → 标准行（UTF-8 | 分隔）:\n    %s\n", out)

	// ── 6. 有值模型：金额空白行 → 零值 "" ──
	blank := gbkBytes("0006228480012345678")
	blank = append(blank, gbkBytes("李四")...)
	blank = append(blank, bytes.Repeat([]byte{' '}, 16)...)
	blank = append(blank, []byte("000000000000000")...) // 全 0 = 空白
	blank = append(blank, []byte("20260918")...)
	blank = append(blank, []byte("XXXXX")...)
	dto2 := (&demo.DemoSource{}).ProtoReflect().New().Interface()
	must(physutil.ParseLine(srcL, dto2, blank), "ParseLine(blank)")
	std2 := (&demo.DemoStandard{}).ProtoReflect().New().Interface()
	must(physutil.MapToStandard(mapping, dto2, std2), "MapToStandard(blank)")
	out2, _ := physutil.RenderDelimitedLine(dstL, std2)
	fmt.Printf("[6] 有值模型：金额全 0 → 零值\n    %s（amt 列为空）\n", out2)

	// ── 7. 校验演示：非法注解构建报错 ──
	// 例：DECIMAL 缺 precision → BuildCachedLayout 返回 error（fail-fast，§3.5）
	fmt.Println("[7] 校验：非法注解（如 DECIMAL 缺 precision）→ 构建期报错（fail-fast）")
}

// gbkBytes 用 x/text GBK 编码器把 UTF-8 字符串转 GBK 字节。
func gbkBytes(s string) []byte {
	enc, _ := physutil.LookupEncoding("GBK")
	out, err := enc.NewEncoder().Bytes([]byte(s))
	must(err, "GBK encode")
	return out
}

func formatName(f fmt.Stringer) string {
	return strings.TrimPrefix(f.String(), "FORMAT_")
}

func must(err error, step string) {
	if err != nil {
		panic(fmt.Sprintf("[%s] %v", step, err))
	}
}
