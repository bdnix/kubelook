package execs_test

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	// client-go's WebSocket heartbeat ends on its own timer after a stream has finished.
	goleak.VerifyTestMain(m, goleak.IgnoreTopFunction("k8s.io/client-go/tools/remotecommand.(*heartbeat).start"))
}
