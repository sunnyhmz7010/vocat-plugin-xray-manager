package engine

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"testing"
	"time"
)

// 验证分享链接映射出的配置确实被打包版本的 Xray 接受，不连接公网。
func TestXrayAcceptsSupportedConfigs(t *testing.T) {
	core := os.Getenv("XRAY_TEST_BINARY")
	if core == "" {
		t.Skip("设置 XRAY_TEST_BINARY 后执行真实内核测试")
	}
	key := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	cases := map[string]string{
		"vless-packet-none": "vless://" + testID + "@example.com:443?flow=xtls-rprx-vision&fp=chrome&packetEncoding=none&pbk=" + key + "&security=reality&sni=example.com&type=tcp",
		"vless-vision-tls":  "vless://" + testID + "@example.com:443?type=tcp&security=tls&flow=xtls-rprx-vision&sni=example.com&fp=chrome",
		"vless-reality":     "vless://" + testID + "@example.com:443?security=reality&pbk=" + key + "&sid=aabb&sni=example.com&flow=xtls-rprx-vision",
		"vless-ws":          "vless://" + testID + "@example.com:443?type=ws&security=tls&path=%2Fws&host=example.com",
		"vless-grpc":        "vless://" + testID + "@example.com:443?type=grpc&security=tls&serviceName=svc&mode=multi",
		"vless-xhttp":       "vless://" + testID + "@example.com:443?type=xhttp&security=reality&pbk=" + key + "&sni=example.com&path=%2Fup&mode=packet-up",
		"trojan":            "trojan://test@example.com:443",
		"ss":                "ss://aes-128-gcm:test@example.com:8388",
		"ss2022":            "ss://2022-blake3-aes-128-gcm:" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 16)) + "@example.com:8388",
		"vmess":             "vmess://" + base64.RawStdEncoding.EncodeToString([]byte(`{"v":"2","add":"example.com","port":"443","id":"`+testID+`","aid":"0","net":"ws","path":"/ws","tls":"tls","scy":"auto"}`)),
	}
	for name, link := range cases {
		t.Run(name, func(t *testing.T) {
			data, err := Config(Record{Link: link, Port: 12345, Username: "test", Password: "test"})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, core, "run", "-test", "-config", "stdin:", "-format", "json")
			cmd.Stdin = bytes.NewReader(data)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("Xray rejected %s: %v\n%s", name, err, output)
			}
		})
	}
}
