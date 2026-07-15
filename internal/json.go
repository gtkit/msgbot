package internal

import (
	"bytes"
	"encoding/json"
)

// Marshal 将 v 序列化为 JSON，行为对齐各平台 payload 依赖的线上格式：
// 关闭 HTML 转义（`<`、`>`、`&` 保持字面，而非 \uXXXX 转义形式，
// 如飞书 <at> 标签），且不追加结尾换行，可直接用作 HTTP 请求体。
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	// Encoder.Encode 会在末尾追加一个换行，去掉它。
	b := buf.Bytes()
	if n := len(b); n > 0 && b[n-1] == '\n' {
		b = b[:n-1]
	}
	return b, nil
}

// Unmarshal 将 JSON 数据解码进 v，直接委托标准库 encoding/json。
func Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
