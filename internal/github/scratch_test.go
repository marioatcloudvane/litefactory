package github_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	gogithub "github.com/google/go-github/v90/github"
)

func TestScratchNewRequestAbsoluteURL(t *testing.T) {
	client, err := gogithub.NewClient(nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	req, err := client.NewRequest(context.Background(), "GET", "https://objects.githubusercontent.com/foo/bar", nil)
	if err != nil {
		t.Fatalf("NewRequest with absolute URL: %v", err)
	}
	t.Logf("resolved URL: %s", req.URL.String())
	t.Logf("host: %s", req.URL.Host)
}

func TestScratchDoLeavesBodyOpenOn200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fake-image-bytes"))
	}))
	defer srv.Close()

	client, err := gogithub.NewClient(nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	req, err := client.NewRequest(context.Background(), "GET", srv.URL+"/asset", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := client.BareDo(req)
	if err != nil {
		t.Fatalf("BareDo: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	t.Logf("status=%d body=%q", resp.StatusCode, string(body))
}

func TestScratchDoOn404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("plain text not found, not json"))
	}))
	defer srv.Close()

	client, err := gogithub.NewClient(nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	req, err := client.NewRequest(context.Background(), "GET", srv.URL+"/asset", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := client.BareDo(context.Background(), req)
	t.Logf("resp=%+v err=%v errType=%T", resp, err, err)
	if resp != nil {
		t.Logf("status=%d", resp.StatusCode)
	}
}
