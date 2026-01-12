package wecom

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type testCoreHandler struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (h *testCoreHandler) HandleMessage(_ context.Context, _ IncomingMessage) error {
	h.mu.Lock()
	h.calls++
	h.mu.Unlock()
	return h.err
}

func (h *testCoreHandler) Calls() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls
}

func TestCallbackHandler_BodyTooLarge(t *testing.T) {
	t.Parallel()

	crypto := mustTestCrypto(t, "t", "ww123")
	core := &testCoreHandler{}
	deduper := NewDeduper(10 * time.Minute)
	t.Cleanup(deduper.Close)

	h := NewCallbackHandler(CallbackDeps{
		Crypto:       crypto,
		Core:         core,
		Deduper:      deduper,
		MaxBodyBytes: 16,
	})

	req := httptest.NewRequest(http.MethodPost, "/wecom/callback?msg_signature=x&timestamp=1&nonce=1", strings.NewReader(strings.Repeat("a", 64)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestCallbackHandler_DedupByMsgID(t *testing.T) {
	t.Parallel()

	token := "test-token"
	crypto := mustTestCrypto(t, token, "ww123")
	core := &testCoreHandler{}
	deduper := NewDeduper(10 * time.Minute)
	t.Cleanup(deduper.Close)

	h := NewCallbackHandler(CallbackDeps{
		Crypto:  crypto,
		Core:    core,
		Deduper: deduper,
	})

	plain := []byte("<xml>" +
		"<ToUserName><![CDATA[to]]></ToUserName>" +
		"<FromUserName><![CDATA[user]]></FromUserName>" +
		"<CreateTime>1700000000</CreateTime>" +
		"<MsgType><![CDATA[text]]></MsgType>" +
		"<Content><![CDATA[菜单]]></Content>" +
		"<MsgId>12345</MsgId>" +
		"</xml>")
	encrypted := mustEncrypt(t, crypto, plain)

	timestamp := "1700000001"
	nonce := "nonce"
	sig := signature(token, timestamp, nonce, encrypted)

	body := []byte("<xml>" +
		"<ToUserName><![CDATA[to]]></ToUserName>" +
		"<Encrypt><![CDATA[" + encrypted + "]]></Encrypt>" +
		"</xml>")

	req1 := httptest.NewRequest(http.MethodPost, "/wecom/callback?msg_signature="+sig+"&timestamp="+timestamp+"&nonce="+nonce, bytes.NewReader(body))
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d", w1.Code, http.StatusOK)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/wecom/callback?msg_signature="+sig+"&timestamp="+timestamp+"&nonce="+nonce, bytes.NewReader(body))
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("second status = %d, want %d", w2.Code, http.StatusOK)
	}

	if got := core.Calls(); got != 1 {
		t.Fatalf("core calls = %d, want 1", got)
	}
}

func mustTestCrypto(t *testing.T, token string, receiverID string) *Crypto {
	t.Helper()
	rawKey := []byte("0123456789abcdef0123456789abcdef")
	encodingAESKey := strings.TrimRight(base64.StdEncoding.EncodeToString(rawKey), "=")
	crypto, err := NewCrypto(CryptoConfig{
		Token:          token,
		EncodingAESKey: encodingAESKey,
		ReceiverID:     receiverID,
	})
	if err != nil {
		t.Fatalf("NewCrypto() error: %v", err)
	}
	return crypto
}

func mustEncrypt(t *testing.T, crypto *Crypto, plain []byte) string {
	t.Helper()
	encrypted, err := crypto.Encrypt(plain, []byte("0123456789abcdef"))
	if err != nil {
		t.Fatalf("Encrypt() error: %v", err)
	}
	return encrypted
}
