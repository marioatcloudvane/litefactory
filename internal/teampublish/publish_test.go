package teampublish_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/teampublish"
	"github.com/cloudvane/litefactory/internal/teamremote"
)

func TestPublishDeterministicImmutableAndConsumerRoundTrip(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	if err := os.CopyFS(source, os.DirFS("../../teams/source")); err != nil {
		t.Fatal(err)
	}
	a, err := teampublish.Build(t.Context(), source, "go-team", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	again, err := teampublish.Build(t.Context(), source, "go-team", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, again) {
		t.Fatal("identical inputs produced different release bytes")
	}
	output := t.TempDir()
	if err := teampublish.Publish(t.Context(), source, output, "go-team", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(output, "teams.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := teampublish.Publish(t.Context(), source, output, "go-team", "1.0.0"); err == nil {
		t.Fatal("immutable release reuse accepted")
	}
	after, err := os.ReadFile(filepath.Join(output, "teams.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("failed publication changed discovery")
	}
	server := httptest.NewTLSServer(http.FileServer(http.Dir(output)))
	defer server.Close()
	client := teamremote.Client{Transport: server.Client().Transport, CacheRoot: t.TempDir()}
	installed, err := client.Fetch(t.Context(), server.URL+"/teams.json", "go-team", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if installed.Manifest.Version != "1.0.0" || !bytes.Equal(installed.ManifestBytes, a.Manifest) || !reflect.DeepEqual(installed.Files, a.Files) {
		t.Fatal("consumer did not reproduce published bytes")
	}
	if err := teampublish.Publish(t.Context(), source, output, "go-team", "1.1.0"); err != nil {
		t.Fatal(err)
	}
	latest, err := client.Fetch(t.Context(), server.URL+"/teams.json", "go-team", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if latest.Manifest.Version != "1.1.0" {
		t.Fatal("stable release did not advance")
	}
	pinned, err := client.Fetch(t.Context(), server.URL+"/teams.json", "go-team", "1.0.0", installed.ManifestDigest)
	if err != nil || pinned.Manifest.Version != "1.0.0" {
		t.Fatalf("exact pin not retained: %v", err)
	}
	if _, err := client.Fetch(t.Context(), server.URL+"/teams.json", "go-team", "1.1.0", installed.ManifestDigest); err == nil {
		t.Fatal("wrong digest accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := client.Fetch(ctx, server.URL+"/teams.json", "go-team", "", ""); err == nil {
		t.Fatal("cancelled fetch accepted")
	}
	if _, err := (teamremote.Client{}).Fetch(t.Context(), server.URL+"/teams.json", "go-team", "", ""); err == nil {
		t.Fatal("untrusted TLS certificate accepted")
	}
	server.Close()
	if _, err := client.Fetch(t.Context(), server.URL+"/teams.json", "go-team", "", ""); err == nil {
		t.Fatal("unreachable upstream silently fell back to cache")
	}
}
