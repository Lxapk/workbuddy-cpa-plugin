package main

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// 回归：插件默认不得替 CPA 放行请求。
//
// CPA 的认证链在第一个返回「已认证」的 provider 处就停止
// （sdk/access/manager.go），所以这里返回 Authenticated=true 会让 CPA 自己的
// api-keys 校验完全不被执行 —— /v1/* 变成无密钥可调用。默认必须返回未认证，
// 让 CPA 接着往下走。
func TestFrontendAuthNeverShortCircuitsCPA(t *testing.T) {
	resetState()
	callOK(t, pluginabi.MethodPluginRegister, lifecycleRequest{
		ConfigYAML: []byte("api_key: plugin-key\n"),
	})
	for _, req := range []pluginapi.FrontendAuthRequest{
		{Method: http.MethodPost, Path: "/v1/chat/completions"},
		{Method: http.MethodPost, Path: "/v1/chat/completions",
			Headers: http.Header{"Authorization": []string{"Bearer wrong"}}},
		{Method: http.MethodGet, Path: "/v1/models"},
	} {
		raw := callOK(t, pluginabi.MethodFrontendAuthAuthenticate, req)
		var out pluginapi.FrontendAuthResponse
		_ = json.Unmarshal(raw, &out)
		if out.Authenticated {
			t.Fatalf("%s %s：插件默认必须交由 CPA 鉴权，不能自行放行", req.Method, req.Path)
		}
	}
}
