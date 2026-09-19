package netease

import "testing"

func TestParseLRC(t *testing.T) {
	lrc := `[ti:标题]
[00:01.00]第一句
[00:05.50]
[00:10.00][00:20.00]重复句
[01:02.345]  带空白的句子  
`
	lines := parseLRC(lrc)
	want := []LyricLine{
		{1.0, "第一句"},
		{10.0, "重复句"},
		{20.0, "重复句"},
		{62.345, "带空白的句子"},
	}
	if len(lines) != len(want) {
		t.Fatalf("解析出 %d 行，预期 %d 行: %+v", len(lines), len(want), lines)
	}
	for i, w := range want {
		if lines[i] != w {
			t.Fatalf("第 %d 行 = %+v，预期 %+v", i, lines[i], w)
		}
	}
}

func TestMergeTranslation(t *testing.T) {
	lines := parseLRC("[00:01.00]hello\n[00:02.00]world")
	mergeTranslation(lines, "[00:01.00]你好\n[00:03.00]时间戳对不上")
	if lines[0].Text != "hello 你好" {
		t.Fatalf("合并失败: %q", lines[0].Text)
	}
	if lines[1].Text != "world" {
		t.Fatalf("无对应翻译的行不应被修改: %q", lines[1].Text)
	}
}
