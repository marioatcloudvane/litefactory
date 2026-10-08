package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyRenderedDelivery(t *testing.T) {
	for _, condition := range []string{"valid", "changed", "missing", "extra", "symlink", "directory", "unsafe", "hash", "cancelled", "limit"} {
		t.Run(condition, func(t *testing.T) {
			root := t.TempDir()
			data := []byte("verified instructions")
			sum := sha256.Sum256(data)
			d := Delivery{Root: root, Identity: "captured", RenderedFiles: map[string]string{"main.md": hex.EncodeToString(sum[:])}}
			path := filepath.Join(root, "main.md")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			switch condition {
			case "changed":
				if err := os.WriteFile(path, []byte("other instructions"), 0600); err != nil {
					t.Fatal(err)
				}
			case "missing", "directory", "symlink":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if condition == "directory" {
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
				}
				if condition == "symlink" {
					if err := os.Symlink("absent", path); err != nil {
						t.Fatal(err)
					}
				}
			case "extra":
				if err := os.WriteFile(filepath.Join(root, "extra.md"), data, 0600); err != nil {
					t.Fatal(err)
				}
			case "unsafe":
				d.RenderedFiles["../outside"] = d.RenderedFiles["main.md"]
			case "hash":
				d.RenderedFiles["main.md"] = "invalid"
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "limit":
				if err := os.Truncate(path, maxRenderedFileBytes+1); err != nil {
					t.Fatal(err)
				}
			}
			err := VerifyDelivery(ctx, d)
			if condition == "valid" && err != nil {
				t.Fatal(err)
			}
			if condition != "valid" && err == nil {
				t.Fatal("invalid delivery accepted")
			}
		})
	}
}

func TestRenderedManifestCloneOwnership(t *testing.T) {
	original := Delivery{RenderedFiles: map[string]string{"main.md": "original"}}
	clone := original.Clone()
	original.RenderedFiles["main.md"] = "changed"
	if clone.RenderedFiles["main.md"] != "original" {
		t.Fatal("manifest aliases caller metadata")
	}
}
