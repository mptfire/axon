package server

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCheckoutSuccessLimiterConcurrent(t *testing.T) {
	const ip = "192.0.2.188"
	checkoutSuccessMu.Lock()
	delete(checkoutSuccessAttempts, ip)
	checkoutSuccessMu.Unlock()
	var allowed atomic.Int64
	var workers sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 32; i++ {
		workers.Go(func() {
			<-start
			if checkoutSuccessAllowed(ip) {
				allowed.Add(1)
			}
		})
	}
	close(start)
	workers.Wait()
	require.EqualValues(t, checkoutSuccessMaxAttempts, allowed.Load())
}

func TestCheckoutSuccessLimiterReapsInactiveIPs(t *testing.T) {
	const expiredIP = "192.0.2.189"
	checkoutSuccessMu.Lock()
	checkoutSuccessAttempts[expiredIP] = []time.Time{time.Now().Add(-2 * time.Hour)}
	checkoutSuccessCleanup = time.Now().Add(-2 * time.Hour)
	checkoutSuccessMu.Unlock()
	checkoutSuccessAllowed("192.0.2.190")
	checkoutSuccessMu.Lock()
	_, retained := checkoutSuccessAttempts[expiredIP]
	checkoutSuccessMu.Unlock()
	require.False(t, retained, "inactive IPs must expire without being seen again")
}
