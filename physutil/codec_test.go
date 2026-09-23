package physutil

// ── 编码（x/text 体系）测试索引 ──
//   TestEncoding_GBK_FixedParse     定长 GBK：字段级解码（先切后解码），中文多字节
//   TestEncoding_GBK_Render         定长 GBK：先编码后 pad（字节宽度语义）
//   TestEncoding_GBK_Delimited      分隔符 GBK：整行解码后 Split
//   TestEncoding_Unknown            未知编码 → 构建报错
//   TestEncoding_UTF8_Invalid       UTF-8 声明但字节非法 → U+FFFD 检测报错（fail-fast）
//   TestEncoding_Reuse_Concurrent   缓存单例 decoder/encoder 并发复用正确性（-race）

import (
	"bytes"
	"fmt"
	"sync"
	"testing"

	"github.com/aif-go/agproto-spec/physutil/internal/testdata"
)

// gbkNameBytes = "张三" 的 GBK 字节（D5C5 C8FD）
var gbkNameBytes = []byte{0xD5, 0xC5, 0xC8, 0xFD}

func TestEncoding_GBK_FixedParse(t *testing.T) {
	l, err := BuildCachedLayout(md(t, &testdata.EncFixed{}))
	if err != nil {
		t.Fatal(err)
	}
	if l.Encoding() != "GBK" {
		t.Errorf("encoding = %q, want GBK", l.Encoding())
	}
	// 行：name 20 字节（"张三" GBK 4 字节 + 16 空格）+ card 19 字节
	line := append(append([]byte{}, gbkNameBytes...), bytes.Repeat([]byte{' '}, 16)...)
	line = append(line, "0006228480012345678"...)
	dest := (&testdata.EncFixed{}).ProtoReflect().New().Interface()
	if err := ParseLine(l, dest, line); err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	fields := dest.ProtoReflect().Descriptor().Fields()
	if got := dest.ProtoReflect().Get(fields.ByName("name")).String(); got != "张三" {
		t.Errorf("name = %q, want 张三（GBK 字段级解码）", got)
	}
	if got := dest.ProtoReflect().Get(fields.ByName("card")).String(); got != "6228480012345678" {
		t.Errorf("card = %q", got)
	}
}

func TestEncoding_GBK_Render(t *testing.T) {
	l, err := BuildCachedLayout(md(t, &testdata.EncFixed{}))
	if err != nil {
		t.Fatal(err)
	}
	m := (&testdata.EncFixed{}).ProtoReflect().New().Interface()
	setStr(m, "name", "张三")
	setStr(m, "card", "6228480012345678")
	out, err := RenderLine(l, m)
	if err != nil {
		t.Fatal(err)
	}
	// name UTF-8 6 字节 → GBK 4 字节 + pad 16 空格 = 20；card 19 字节
	want := append(append([]byte{}, gbkNameBytes...), bytes.Repeat([]byte{' '}, 16)...)
	want = append(want, "0006228480012345678"...)
	if !bytes.Equal(out, want) {
		t.Errorf("render = %x\nwant   %x", out, want)
	}
	if len(out) != 39 {
		t.Errorf("line length = %d, want 39（GBK 字节宽度语义）", len(out))
	}
}

func TestEncoding_GBK_Delimited(t *testing.T) {
	l, err := BuildCachedLayout(md(t, &testdata.EncDelimited{}))
	if err != nil {
		t.Fatal(err)
	}
	// 文件 = GBK 编码的 "张三|6228"（编码器生成，验证整行解码后 Split）
	utf8line := "张三|6228"
	gbk, err := l.encoder.Bytes([]byte(utf8line))
	if err != nil {
		t.Fatal(err)
	}
	dest := (&testdata.EncDelimited{}).ProtoReflect().New().Interface()
	if err := ParseDelimitedLine(l, dest, gbk); err != nil {
		t.Fatalf("ParseDelimitedLine: %v", err)
	}
	fields := dest.ProtoReflect().Descriptor().Fields()
	if got := dest.ProtoReflect().Get(fields.ByName("name")).String(); got != "张三" {
		t.Errorf("name = %q, want 张三", got)
	}
	if got := dest.ProtoReflect().Get(fields.ByName("card")).String(); got != "6228" {
		t.Errorf("card = %q", got)
	}
}

func TestEncoding_Unknown(t *testing.T) {
	_, err := BuildCachedLayout(md(t, &testdata.EncUnknown{}))
	if err == nil {
		t.Fatal("expected unsupported encoding error")
	}
}

func TestEncoding_UTF8_Invalid(t *testing.T) {
	// 声明 UTF-8 但字节非法（GBK 双字节）→ x/text 替换为 U+FFFD → checkFFFD 报错
	l, err := BuildCachedLayout(md(t, &testdata.ValidFixed{}))
	if err != nil {
		t.Fatal(err)
	}
	line := append(append([]byte{}, gbkNameBytes...), bytes.Repeat([]byte{' '}, 60)...)
	dest := (&testdata.ValidFixed{}).ProtoReflect().New().Interface()
	if err := ParseLine(l, dest, line); err == nil {
		t.Fatal("expected U+FFFD encoding mismatch error")
	}
}

func TestEncoding_Reuse_Concurrent(t *testing.T) {
	// 缓存单例 decoder/encoder 并发复用（-race 检测）：16 goroutine × 2000 次共享 layout
	const goroutines = 16
	const iters = 2000
	l, err := BuildCachedLayout(md(t, &testdata.EncFixed{}))
	if err != nil {
		t.Fatal(err)
	}
	line := append(append([]byte{}, gbkNameBytes...), bytes.Repeat([]byte{' '}, 16)...)
	line = append(line, "0006228480012345678"...)

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				dest := (&testdata.EncFixed{}).ProtoReflect().New().Interface()
				if err := ParseLine(l, dest, line); err != nil {
					errCh <- err
					return
				}
				if got := dest.ProtoReflect().Get(dest.ProtoReflect().Descriptor().Fields().ByName("name")).String(); got != "张三" {
					errCh <- fmt.Errorf("name mismatch: %q", got)
					return
				}
				out, err := RenderLine(l, dest)
				if err != nil {
					errCh <- err
					return
				}
				if !bytes.Equal(out, line) {
					errCh <- fmt.Errorf("render mismatch: %x", out)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}
