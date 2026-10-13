package user

import (
	"errors"
	"net/netip"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManager_DeviceQuotaConcurrent(t *testing.T) {
	forEachBackend(t, func(t *testing.T, newManager newManagerFunc) {
		for _, mode := range []string{"build", "pairing"} {
			t.Run(mode, func(t *testing.T) {
				a := newTestManager(t, newManager, PermissionDenyAll)
				b := newTestManager(t, newManager, PermissionDenyAll)
				owner := "alice-" + mode
				require.NoError(t, a.AddUser(owner, "alice", RoleUser, false))
				for i := 0; i < deviceMaxPerUser-1; i++ {
					_, _, err := a.ClaimBuildKey(owner, "phone", netip.IPv4Unspecified())
					require.NoError(t, err)
				}
				u, err := a.User(owner)
				require.NoError(t, err)
				codes := make([]string, 32)
				if mode == "pairing" {
					for i := range codes {
						code, err := a.CreatePairingCode(u.ID, "phone")
						require.NoError(t, err)
						codes[i] = code.Code
					}
				}
				start := make(chan struct{})
				failures := make(chan error, len(codes))
				var workers sync.WaitGroup
				for i := range codes {
					workers.Go(func() {
						<-start
						manager := a
						if i%2 == 1 {
							manager = b
						}
						var err error
						if mode == "pairing" {
							_, _, err = manager.ClaimPairingCode(codes[i], "phone", netip.IPv4Unspecified())
						} else {
							_, _, err = manager.ClaimBuildKey(owner, "phone", netip.IPv4Unspecified())
						}
						if err != nil && !errors.Is(err, ErrTooManyDevices) {
							failures <- err
						}
					})
				}
				close(start)
				workers.Wait()
				close(failures)
				for err := range failures {
					require.NoError(t, err)
				}
				devices, err := a.Devices(u.ID)
				require.NoError(t, err)
				require.Len(t, devices, deviceMaxPerUser)
			})
		}
	})
}
