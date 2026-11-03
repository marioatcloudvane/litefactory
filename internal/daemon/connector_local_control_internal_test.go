package daemon

import (
	"net"
	"testing"
)

func TestFactoryConnectorComposesShippedLocalControl(t *testing.T) {
	t.Parallel()
	s := remoteSetupFixture(t)
	s.ConnectorControl = nil
	s.Config.RemoteConnector.ListenAddr = connectorTestListenAddress(t)
	runtime, err := prepareFactoryRemoteConnector(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	if runtime == nil || runtime.server == nil || runtime.lifecycle == nil {
		t.Fatal("factory did not compose the real local control capability")
	}
	if err := runtime.lifecycle.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFactoryConnectorLocalControlDoesNotBypassPrerequisites(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"storage unavailable", "owner mismatch", "secret unavailable", "disabled"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			s := remoteSetupFixture(t)
			s.ConnectorControl = nil
			s.Config.RemoteConnector.ListenAddr = connectorTestListenAddress(t)
			switch state {
			case "storage unavailable":
				if err := s.DB.Close(); err != nil {
					t.Fatal(err)
				}
			case "owner mismatch":
				s.Config.RemoteConnector.OwnerID = 43
			case "secret unavailable":
				s.ConnectionSecrets.LookupEnv = func(string) (string, bool) { return "", false }
			case "disabled":
				s.Config.RemoteConnector.Enabled = false
			}
			runtime, err := prepareFactoryRemoteConnector(t.Context(), s)
			if runtime != nil {
				_ = runtime.lifecycle.Close()
				t.Fatal("unavailable prerequisites still bound remote listener")
			}
			if state == "disabled" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("unavailable prerequisites accepted")
			}
		})
	}
}

func connectorTestListenAddress(t *testing.T) string {
	t.Helper()
	var lc net.ListenConfig
	listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}
