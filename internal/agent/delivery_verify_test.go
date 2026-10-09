package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyRenderedDelivery(t *testing.T) {
	for _, condition := range []string{"valid", "changed", "missing", "extra", "extra directory", "symlink", "directory", "unsafe", "hash", "cancelled", "limit"} {
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
			case "extra directory":
				if err := os.Mkdir(filepath.Join(root, "extra"), 0700); err != nil {
					t.Fatal(err)
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

func TestRenderedDeliveryAggregateAndEntryLimits(t *testing.T) {
	t.Run("aggregate bytes", func(t *testing.T) {
		root := t.TempDir()
		data := make([]byte, maxRenderedFileBytes)
		sum := sha256.Sum256(data)
		d := Delivery{Root: root, Identity: "bound", RenderedFiles: map[string]string{}}
		for i := 0; i < 9; i++ {
			name := fmt.Sprintf("resource-%d.md", i)
			if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
				t.Fatal(err)
			}
			d.RenderedFiles[name] = hex.EncodeToString(sum[:])
		}
		if err := VerifyDelivery(t.Context(), d); err == nil || !strings.Contains(err.Error(), "read limit") {
			t.Fatalf("aggregate limit not enforced: %v", err)
		}
	})
	t.Run("manifest entries", func(t *testing.T) {
		d := Delivery{Root: t.TempDir(), Identity: "bound", RenderedFiles: map[string]string{}}
		for i := 0; i <= maxRenderedEntries; i++ {
			d.RenderedFiles[fmt.Sprintf("resource-%d.md", i)] = strings.Repeat("a", 64)
		}
		if err := VerifyDelivery(t.Context(), d); err == nil || !strings.Contains(err.Error(), "bounded") {
			t.Fatalf("manifest bound not enforced: %v", err)
		}
	})
	t.Run("root symlink", func(t *testing.T) {
		root := t.TempDir()
		link := filepath.Join(t.TempDir(), "linked")
		if err := os.Symlink(root, link); err != nil {
			t.Fatal(err)
		}
		d := Delivery{Root: link, Identity: "bound", RenderedFiles: map[string]string{"main.md": strings.Repeat("a", 64)}}
		if err := VerifyDelivery(t.Context(), d); err == nil || !strings.Contains(err.Error(), "directory") {
			t.Fatalf("symlink root accepted: %v", err)
		}
	})
}

// Cancelling on a context checkpoint proves cancellation inside the read loop
// without a clock, goroutine or timing-dependent filesystem mutation.
type readCancellingContext struct {
	context.Context
	cancel context.CancelFunc
	checks int
}

func (c *readCancellingContext) Err() error {
	c.checks++
	if c.checks == 8 {
		c.cancel()
	}
	return c.Context.Err()
}
func TestRenderedDeliveryCancellationDuringRead(t *testing.T) {
	root := t.TempDir()
	data := make([]byte, maxRenderedFileBytes)
	sum := sha256.Sum256(data)
	if err := os.WriteFile(filepath.Join(root, "main.md"), data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	checked := &readCancellingContext{Context: ctx, cancel: cancel}
	d := Delivery{Root: root, Identity: "bound", RenderedFiles: map[string]string{"main.md": hex.EncodeToString(sum[:])}}
	if err := VerifyDelivery(checked, d); !errors.Is(err, context.Canceled) {
		t.Fatalf("read cancellation ignored: %v", err)
	}
}

func TestRenderedDeliveryBoundsUndeclaredDirectoryEntries(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < maxRenderedEntries; i++ {
		if err := os.Mkdir(filepath.Join(root, fmt.Sprintf("extra-%d", i)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	d := Delivery{Root: root, Identity: "bound", RenderedFiles: map[string]string{"main.md": strings.Repeat("a", 64)}}
	if err := VerifyDelivery(t.Context(), d); err == nil || !strings.Contains(err.Error(), "entry limit") {
		t.Fatalf("undeclared directory entries unbounded: %v", err)
	}
}
