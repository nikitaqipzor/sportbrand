package memory_test

import (
	"testing"

	"athletica.ai/api/internal/store"
	"athletica.ai/api/internal/store/memory"
	"athletica.ai/api/internal/store/storetest"
)

func TestMemoryStoreConformance(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store { return memory.New() })
}

func TestPingAndCloseAreSafe(t *testing.T) {
	st := memory.New()
	if err := st.Ping(t.Context()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	st.Close()
}
