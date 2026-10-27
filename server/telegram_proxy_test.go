package main

import (
	"net/http"
	"testing"
)

func TestNewTelegramProxy(t *testing.T) {
	tg, err := newTelegram("T", "")
	if err != nil || tg.hc.Transport != nil {
		t.Fatalf("без прокси: err=%v transport=%v", err, tg.hc.Transport)
	}
	tg, err = newTelegram("T", "socks5h://u:p@127.0.0.1:1080")
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, "https://api.telegram.org/", nil)
	u, err := tg.hc.Transport.(*http.Transport).Proxy(req)
	if err != nil || u == nil || u.Host != "127.0.0.1:1080" {
		t.Fatalf("прокси: %v %v", u, err)
	}
	for _, bad := range []string{"ftp://h:1", "socks5://", "://x"} {
		if _, err := newTelegram("T", bad); err == nil {
			t.Errorf("%q: ждали ошибку", bad)
		}
	}
}
