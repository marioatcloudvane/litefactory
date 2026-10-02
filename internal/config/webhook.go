package config

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
)

// DefaultWebhookListen is the address the webhook listener binds when the key
// is absent. It is loopback only: exposing it to a network is the operator's
// decision, made by writing another address.
const DefaultWebhookListen = "127.0.0.1:8787"

// Webhook configures the optional GitHub webhook listener.
type Webhook struct {
	// Listen is a host:port the listener binds. Empty turns the listener off.
	Listen string `yaml:"listen"`
	// PublicURL is the address GitHub delivers webhooks to: an absolute http or
	// https URL that reaches the listener. Empty means litefactory registers no
	// webhooks and repositories keep working on polling alone.
	PublicURL string `yaml:"public_url"`
}

func (w Webhook) validatePublicURL() []error {
	if w.PublicURL == "" {
		return nil
	}
	u, err := url.Parse(w.PublicURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return []error{fmt.Errorf("webhook.public_url: %q is not an absolute http or https URL", w.PublicURL)}
	}
	return nil
}

func (w Webhook) validate() []error {
	errs := w.validatePublicURL()
	if w.Listen == "" {
		return errs
	}
	host, port, err := net.SplitHostPort(w.Listen)
	if err != nil {
		return append(errs, fmt.Errorf("webhook.listen: %q is not a host:port address: %v", w.Listen, err))
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return append(errs, fmt.Errorf("webhook.listen: port %q must be a number between 1 and 65535", port))
	}
	if host == "" {
		return append(errs, fmt.Errorf("webhook.listen: %q has no host; write the interface explicitly, e.g. 127.0.0.1:%d", w.Listen, n))
	}
	return errs
}
