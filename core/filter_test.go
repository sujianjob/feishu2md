package core

import (
	"path/filepath"
	"testing"
)

func TestParsePatterns(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"", nil},
		{"test", []string{"test"}},
		{"test,demo", []string{"test", "demo"}},
		{"test, demo, ", []string{"test", "demo"}},
		{"*测试*,*草稿*", []string{"*测试*", "*草稿*"}},
		{"  spaced  ,  patterns  ", []string{"spaced", "patterns"}},
	}

	for _, tt := range tests {
		result := ParsePatterns(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("ParsePatterns(%q) = %v, want %v", tt.input, result, tt.expected)
			continue
		}
		for i, v := range result {
			if v != tt.expected[i] {
				t.Errorf("ParsePatterns(%q)[%d] = %q, want %q", tt.input, i, v, tt.expected[i])
			}
		}
	}
}

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    bool
	}{
		{"test", "test", true},
		{"testing", "test*", true},
		{"mytest", "*test", true},
		{"mytesting", "*test*", true},
		{"测试文档", "*测试*", true},
		{"文档", "*测试*", false},
		{"draft-v1", "draft-*", true},
		{"abc", "???", true},
		{"ab", "???", false},
		{"file.txt", "*.txt", true},
		{"file.md", "*.txt", false},
		{"doc", "doc", true},
		{"docs", "doc", false},
	}

	for _, tt := range tests {
		got := matchPattern(tt.name, tt.pattern)
		if got != tt.want {
			t.Errorf("matchPattern(%q, %q) = %v, want %v",
				tt.name, tt.pattern, got, tt.want)
		}
	}
}

func TestMatchAnyPattern(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		want     bool
	}{
		{"test", []string{"test", "demo"}, true},
		{"demo", []string{"test", "demo"}, true},
		{"other", []string{"test", "demo"}, false},
		{"testing", []string{"test*", "demo*"}, true},
		{"文档", []string{"*测试*", "*文档*"}, true},
		{"any", []string{}, false},
	}

	for _, tt := range tests {
		got := matchAnyPattern(tt.name, tt.patterns)
		if got != tt.want {
			t.Errorf("matchAnyPattern(%q, %v) = %v, want %v",
				tt.name, tt.patterns, got, tt.want)
		}
	}
}

func TestNodeFilter_ShouldIncludeNode_IncludeOnly(t *testing.T) {
	filter := NewNodeFilter(FilterConfig{
		IncludePatterns: []string{"*文档*", "API*"},
	})

	tests := []struct {
		parent  string
		name    string
		include bool
	}{
		{"", "技术文档", true},
		{"", "API指南", true},
		{"", "草稿", false},
		{"", "其他资料", false},
	}

	for _, tt := range tests {
		include, _ := filter.ShouldIncludeNode(tt.parent, tt.name)
		if include != tt.include {
			t.Errorf("ShouldIncludeNode(%q, %q) = %v, want %v",
				tt.parent, tt.name, include, tt.include)
		}
	}
}

func TestNodeFilter_ShouldIncludeNode_ExcludeOnly(t *testing.T) {
	filter := NewNodeFilter(FilterConfig{
		ExcludePatterns: []string{"*草稿*", "*测试*"},
	})

	tests := []struct {
		parent  string
		name    string
		include bool
	}{
		{"", "正式文档", true},
		{"", "测试文档", false},
		{"", "草稿", false},
		{"", "产品草稿", false},
	}

	for _, tt := range tests {
		include, _ := filter.ShouldIncludeNode(tt.parent, tt.name)
		if include != tt.include {
			t.Errorf("ShouldIncludeNode(%q, %q) = %v, want %v",
				tt.parent, tt.name, include, tt.include)
		}
	}
}

func TestNodeFilter_ShouldIncludeNode_IncludeAndExclude(t *testing.T) {
	filter := NewNodeFilter(FilterConfig{
		IncludePatterns: []string{"*文档*"},
		ExcludePatterns: []string{"*草稿*"},
	})

	tests := []struct {
		parent  string
		name    string
		include bool
	}{
		{"", "技术文档", true},
		{"", "草稿文档", false}, // 被 exclude 排除
		{"", "其他资料", false}, // 不匹配 include
	}

	for _, tt := range tests {
		include, _ := filter.ShouldIncludeNode(tt.parent, tt.name)
		if include != tt.include {
			t.Errorf("ShouldIncludeNode(%q, %q) = %v, want %v",
				tt.parent, tt.name, include, tt.include)
		}
	}
}

func TestNodeFilter_ShouldIncludeNode_ParentExcluded(t *testing.T) {
	filter := NewNodeFilter(FilterConfig{
		ExcludePatterns: []string{"草稿"},
	})

	// 排除"草稿"目录
	include, skipped := filter.ShouldIncludeNode("wiki", "草稿")
	if include {
		t.Error("Expected '草稿' to be excluded")
	}
	if skipped {
		t.Error("Expected skippedByParent to be false for first exclusion")
	}

	// 子目录应该因父目录被排除而跳过
	// 使用 filepath.Join 确保路径分隔符一致
	parentPath := filepath.Join("wiki", "草稿")
	include, skipped = filter.ShouldIncludeNode(parentPath, "子文件夹")
	if include {
		t.Error("Expected '子文件夹' to be excluded because parent is excluded")
	}
	if !skipped {
		t.Error("Expected skippedByParent to be true")
	}

	// 深层嵌套也应该被跳过
	deepParentPath := filepath.Join("wiki", "草稿", "子文件夹")
	include, skipped = filter.ShouldIncludeNode(deepParentPath, "深层文件夹")
	if include {
		t.Error("Expected '深层文件夹' to be excluded because ancestor is excluded")
	}
	if !skipped {
		t.Error("Expected skippedByParent to be true for deeply nested")
	}
}

func TestNodeFilter_NoFilters(t *testing.T) {
	filter := NewNodeFilter(FilterConfig{})

	include, _ := filter.ShouldIncludeNode("", "任意目录")
	if !include {
		t.Error("Expected all nodes to be included when no filters")
	}

	if filter.HasFilters() {
		t.Error("Expected HasFilters() to return false")
	}
}

func TestNodeFilter_HasFilters(t *testing.T) {
	tests := []struct {
		config FilterConfig
		want   bool
	}{
		{FilterConfig{}, false},
		{FilterConfig{IncludePatterns: []string{"test"}}, true},
		{FilterConfig{ExcludePatterns: []string{"test"}}, true},
		{FilterConfig{IncludePatterns: []string{"a"}, ExcludePatterns: []string{"b"}}, true},
	}

	for _, tt := range tests {
		filter := NewNodeFilter(tt.config)
		if got := filter.HasFilters(); got != tt.want {
			t.Errorf("HasFilters() with %+v = %v, want %v", tt.config, got, tt.want)
		}
	}
}

func TestNodeFilter_ShouldDownloadFolder(t *testing.T) {
	filter := NewNodeFilter(FilterConfig{
		ExcludePatterns: []string{"*draft*"},
	})

	if !filter.ShouldDownloadFolder("", "docs") {
		t.Error("Expected 'docs' folder to be downloadable")
	}

	if filter.ShouldDownloadFolder("", "draft-v1") {
		t.Error("Expected 'draft-v1' folder to be excluded")
	}
}

func TestNodeFilter_ShouldDownloadDocument(t *testing.T) {
	filter := NewNodeFilter(FilterConfig{
		ExcludePatterns: []string{"草稿"},
	})

	// 排除"草稿"目录
	filter.ShouldIncludeNode("wiki", "草稿")

	// 文档在正常路径下应该可以下载
	normalPath := filepath.Join("wiki", "正式")
	if !filter.ShouldDownloadDocument(normalPath) {
		t.Error("Expected document in 'wiki/正式' to be downloadable")
	}

	// 文档在被排除的路径下不应该下载
	excludedPath := filepath.Join("wiki", "草稿")
	if filter.ShouldDownloadDocument(excludedPath) {
		t.Error("Expected document in 'wiki/草稿' to be excluded")
	}
}

func TestNodeFilter_Reset(t *testing.T) {
	filter := NewNodeFilter(FilterConfig{
		ExcludePatterns: []string{"test"},
	})

	// 排除一个目录
	filter.ShouldIncludeNode("", "test")

	// 子目录应该被跳过（因为父目录被排除）
	include, skipped := filter.ShouldIncludeNode("test", "child")
	if include {
		t.Error("Expected child to be excluded before reset")
	}
	if !skipped {
		t.Error("Expected child to be skipped by parent before reset")
	}

	// 重置
	filter.Reset()

	// 重置后，"test" 目录仍然会被排除（因为匹配规则），但不是因为缓存
	include, _ = filter.ShouldIncludeNode("", "test")
	if include {
		t.Error("Expected 'test' to still be excluded by pattern after reset")
	}

	// 现在 "test/child" 会因为父目录被排除而跳过
	include, skipped = filter.ShouldIncludeNode("test", "child")
	if include {
		t.Error("Expected child to be excluded because parent matches exclude pattern")
	}
	if !skipped {
		t.Error("Expected child to be skipped by parent after reset")
	}
}
