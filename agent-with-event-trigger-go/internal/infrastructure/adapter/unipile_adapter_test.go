package adapter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/mailbox"
)

func TestUnipileRequiresCredentials(t *testing.T) {
	if _, err := NewUnipileClient(UnipileConfig{DSN: "api.example.invalid"}); err == nil {
		t.Fatal("缺少 token 时应报错")
	}
}

// 401 要留下回执再抛错：实验终态需要凭据失败的证据，而不是一条空日志。
func TestUnipileAPIErrorIsReceiptedAndReturned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-KEY") != "unit-secret" {
			t.Errorf("缺少凭据头，实际 = %q", r.Header.Get("X-API-KEY"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"status":401,"type":"errors/missing_credentials",` +
			`"title":"Missing credentials"}`))
	}))
	defer server.Close()

	client, err := NewUnipileClient(UnipileConfig{DSN: server.URL, AccessToken: "unit-secret"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	_, err = client.ListAccounts(context.Background())
	var apiErr *mailbox.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("错误类型 = %T，期望 *mailbox.APIError", err)
	}
	if apiErr.Status != 401 || apiErr.Type != "errors/missing_credentials" {
		t.Errorf("错误详情 = %+v", apiErr)
	}

	calls := client.Calls()
	if len(calls) != 1 {
		t.Fatalf("回执数 = %d，期望 1", len(calls))
	}
	if calls[0].Success || calls[0].Status == nil || *calls[0].Status != 401 {
		t.Errorf("回执 = %+v，应记录失败与状态码", calls[0])
	}
	if calls[0].CredentialScheme != "X-API-KEY" {
		t.Errorf("回执应记录凭据方案，实际 = %q", calls[0].CredentialScheme)
	}
	if encoded := mailbox.CanonicalJSONString(calls); strings.Contains(encoded, "unit-secret") {
		t.Errorf("回执泄露了明文凭据: %s", encoded)
	}
}

func TestUnipileListAccountsParsesItems(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/accounts" {
			t.Errorf("请求路径 = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"account-real","email":"a@example.com"}]}`))
	}))
	defer server.Close()

	client, err := NewUnipileClient(UnipileConfig{DSN: server.URL, AccessToken: "unit-secret"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	accounts, err := client.ListAccounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 || mailbox.StringField(accounts[0], "id") != "account-real" {
		t.Fatalf("解析结果 = %v", accounts)
	}
	if !client.Calls()[0].Success {
		t.Error("成功调用应记录 success=true")
	}
}

// 发信端点返回 201 才算成功，按 200 判定会把成功当失败。
func TestUnipileSendEmailExpects201(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"object":"EmailSent"}`))
	}))
	defer server.Close()

	client, err := NewUnipileClient(UnipileConfig{DSN: server.URL, AccessToken: "unit-secret"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	receipt, err := client.SendEmail(context.Background(), "account-real",
		"to@example.com", "主题", "正文")
	if err != nil {
		t.Fatalf("201 不应被当成错误: %v", err)
	}
	if mailbox.StringField(receipt, "object") != "EmailSent" {
		t.Errorf("回执 = %v", receipt)
	}
}

// DSN 不带协议时要补 https，否则请求会直接失败。
func TestUnipileNormalizesDSNScheme(t *testing.T) {
	client, err := NewUnipileClient(UnipileConfig{
		DSN: "api.example.invalid:12345", AccessToken: "unit-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if client.BaseURL() != "https://api.example.invalid:12345" {
		t.Errorf("端点 = %q", client.BaseURL())
	}
}
