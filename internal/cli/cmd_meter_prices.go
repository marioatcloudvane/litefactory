package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/meter"
	"github.com/cloudvane/litefactory/internal/meterview"
	"github.com/cloudvane/litefactory/internal/store"
)

func newMeterPricesCommand() *Command {
	return &Command{Name: "meter prices", Usage: "meter prices <list|set>", Short: "Manage immutable local price catalogs", New: func(*flag.FlagSet) Handler {
		return func(_ context.Context, env *Env, _ []string) error {
			_, err := fmt.Fprintln(env.Stdout, "Usage: litefactory meter prices list [--json]\n       litefactory meter prices set MODEL --coding_agent AGENT --input RATE --output RATE [--cache_read RATE] [--cache_write RATE] [--cache_write_5m RATE] [--cache_write_1h RATE]\nRates are exact USD per million tokens. Edits apply only to later attempts.")
			return err
		}
	}}
}
func newMeterPricesListCommand() *Command {
	return &Command{Name: "meter prices list", Usage: "meter prices list [--json]", Short: "Show the current local price revision", New: func(fs *flag.FlagSet) Handler {
		var asJSON bool
		fs.BoolVar(&asJSON, "json", false, "emit JSON")
		return func(ctx context.Context, env *Env, _ []string) (err error) {
			rt, err := openRepoRuntime(ctx)
			if err != nil {
				return err
			}
			defer func() { err = errors.Join(err, rt.close()) }()
			return writeMeterPrices(ctx, env, rt.db, asJSON)
		}
	}}
}
func writeMeterPrices(ctx context.Context, env *Env, db *store.DB, asJSON bool) error {
	catalog, err := meterview.Prices(ctx, db, "", "")
	if err != nil {
		return err
	}
	if asJSON {
		return json.NewEncoder(env.Stdout).Encode(catalog)
	}
	for _, line := range meterview.PriceLines(catalog) {
		if _, err := fmt.Fprintln(env.Stdout, line); err != nil {
			return err
		}
	}
	return nil
}
func newMeterPricesSetCommand() *Command {
	return &Command{Name: "meter prices set", Usage: "meter prices set <model> --coding_agent AGENT --input RATE --output RATE [--cache_read RATE] [--cache_write RATE] [--cache_write_5m RATE] [--cache_write_1h RATE]", Short: "Create a price revision replacing one agent/model entry", Args: ArgSpec{Names: []string{"model"}, Min: 1}, New: func(fs *flag.FlagSet) Handler {
		var codingAgent string
		fs.StringVar(&codingAgent, "coding_agent", "", "claude or codex")
		values := map[string]*string{}
		for _, dimension := range meter.PriceDimensions() {
			name := dimension
			if name == "input_uncached" {
				name = "input"
			}
			value := new(string)
			values[dimension] = value
			fs.Func(name, "exact USD per million tokens", func(raw string) error {
				if raw == "" {
					return errors.New("rate must not be empty")
				}
				*value = raw
				return nil
			})
		}
		return func(ctx context.Context, env *Env, args []string) (err error) {
			if len(args) != 1 {
				return Usagef(nil, "one model is required")
			}
			rates, err := meterPriceArguments(codingAgent, args[0], values)
			if err != nil {
				return err
			}
			rt, err := openRepoRuntime(ctx)
			if err != nil {
				return err
			}
			defer func() { err = errors.Join(err, rt.close()) }()
			revision, err := rt.db.SetMeterPrices(ctx, codingAgent, args[0], rates)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(env.Stdout, "Created price revision %d; existing attempts retain their pinned prices.\n", revision)
			return err
		}
	}}
}
func meterPriceArguments(codingAgent, model string, values map[string]*string) ([]store.MeterPriceRate, error) {
	if codingAgent != "claude" && codingAgent != "codex" {
		return nil, Usagef(nil, "coding_agent must be claude or codex")
	}
	if model == "" || len(model) > 256 || strings.ContainsAny(model, "\x00\r\n\t") {
		return nil, Usagef(nil, "invalid model")
	}
	rates := []store.MeterPriceRate{}
	for _, dimension := range meter.PriceDimensions() {
		value := values[dimension]
		if value == nil || *value == "" {
			if dimension == "input_uncached" || dimension == "output" {
				return nil, Usagef(nil, "input and output rates are required")
			}
			continue
		}
		canonical, err := meter.CanonicalRate(*value)
		if err != nil {
			return nil, Usagef(nil, "%s: %v", dimension, err)
		}
		rates = append(rates, store.MeterPriceRate{CodingAgent: codingAgent, Model: model, Dimension: dimension, USDPerMillion: canonical})
	}
	return rates, nil
}
