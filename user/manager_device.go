package user

import (
	"database/sql"
	"errors"
	"net/netip"
	"time"

	"heckel.io/ntfy/v2/db"
	"heckel.io/ntfy/v2/util"
)

// Device registry and pairing flow for the axon agent channel
// (docs/agents.md). A paired device is an app instance that exchanged a
// one-time pairing code for its own device-scoped access token. The token's
// scopes contain only TokenScopeDevice, so route-level gating can tell a
// device apart from an unrestricted user token.

const (
	deviceIDPrefix = "dv_"
	deviceIDLength = 8
	pairingCodeLen = 8 // human-typed; short-lived and one-time
	// PairingCodeTTL is how long a minted pairing code can be claimed.
	PairingCodeTTL = 5 * time.Minute
	// DeviceConfigMaxBytes caps the PUT /v1/device/{id}/config body.
	DeviceConfigMaxBytes = 64 * 1024
	deviceMaxPerUser     = 10
)

var (
	// ErrPairingCodeInvalid means the code is unknown, expired, or already used.
	ErrPairingCodeInvalid = errors.New("pairing code invalid, expired, or already used")
	// ErrDeviceNotFound means no such device for that user.
	ErrDeviceNotFound = errors.New("device not found")
	// ErrTooManyDevices means the per-user device budget is exhausted.
	ErrTooManyDevices = errors.New("too many devices")
)

// CreatePairingCode mints a one-time, short-lived code for the given user. The
// code is the only credential needed to claim it, which is why minting requires
// an authenticated (non-device-scoped) caller and the TTL is minutes, not hours.
// Expired and long-consumed codes are pruned on every mint.
func (a *Manager) CreatePairingCode(userID, label string) (*PairingCode, error) {
	pc := &PairingCode{
		Code:      util.RandomLowerStringPrefix("", pairingCodeLen),
		UserID:    userID,
		Label:     label,
		ExpiresAt: time.Now().Add(PairingCodeTTL),
	}
	err := db.ExecTx(a.db, func(tx *sql.Tx) error {
		day := time.Now().Add(-24 * time.Hour).Unix()
		if _, err := tx.Exec(a.queries.deleteExpiredPairing, day, day); err != nil {
			return err
		}
		_, err := tx.Exec(a.queries.insertPairingCode, pc.Code, pc.UserID, pc.Label, pc.ExpiresAt.Unix(), time.Now().Unix())
		return err
	})
	if err != nil {
		return nil, err
	}
	return pc, nil
}

// ClaimPairingCode atomically consumes a pairing code and provisions a device
// with its own device-scoped token. The claim is the human tap: whoever holds
// the code within its validity window gets the token, and the code dies with
// the claim (single use — the conditional UPDATE is the guard, so two racing
// claims cannot both succeed).
func (a *Manager) ClaimPairingCode(code, label string, origin netip.Addr) (*Device, string, error) {
	var dev *Device
	err := db.ExecTx(a.db, func(tx *sql.Tx) error {
		// Atomic single-use guard FIRST: only one racing claim sees 1 row affected
		res, err := tx.Exec(a.queries.usePairingCode, code, time.Now().Unix())
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		} else if n != 1 {
			return ErrPairingCodeInvalid
		}
		var userID string
		if err := tx.QueryRow(a.queries.selectPairingUserID, code).Scan(&userID); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRow(a.queries.selectDeviceCount, userID).Scan(&count); err != nil {
			return err
		} else if count >= deviceMaxPerUser {
			return ErrTooManyDevices
		}
		now := time.Now()
		deviceID := util.RandomStringPrefix(deviceIDPrefix, deviceIDLength+len(deviceIDPrefix))
		if label == "" {
			label = "device " + deviceID[len(deviceIDPrefix):]
		}
		token := GenerateToken()
		if _, err := tx.Exec(a.queries.insertDevice, deviceID, userID, token, label, now.Unix(), now.Unix(), now.Unix()); err != nil {
			return err
		}
		// Device token: never expires (revocation = DELETE /v1/device/{id}),
		// carries only the device scope. Origin is recorded for the audit trail.
		if _, err := tx.Exec(a.queries.upsertToken, userID, token, "device "+deviceID, now.Unix(), origin.String(), 0, false, TokenScopeDevice); err != nil {
			return err
		}
		dev = &Device{ID: deviceID, UserID: userID, Token: token, Label: label, CreatedAt: now, UpdatedAt: now}
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return dev, dev.Token, nil
}

// Devices lists a user's paired devices. Tokens are not exposed.
func (a *Manager) Devices(userID string) ([]*Device, error) {
	rows, err := a.db.Query(a.queries.selectDevices, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	devices := make([]*Device, 0)
	for {
		dev, err := a.readDevice(rows)
		if errors.Is(err, ErrDeviceNotFound) {
			break
		} else if err != nil {
			return nil, err
		}
		devices = append(devices, dev)
	}
	return devices, nil
}

// DeviceByID returns one of the user's devices.
func (a *Manager) DeviceByID(userID, deviceID string) (*Device, error) {
	rows, err := a.db.Query(a.queries.selectDeviceByID, deviceID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return a.readDevice(rows)
}

// DeviceByToken resolves a device-scoped token to its device. Used to enforce
// that a device token may only touch its own device.
func (a *Manager) DeviceByToken(token string) (*Device, error) {
	rows, err := a.db.Query(a.queries.selectDeviceByToken, token)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return a.readDevice(rows)
}

// ChangeDeviceConfig stores the (already validated) config JSON for a device.
func (a *Manager) ChangeDeviceConfig(userID, deviceID, config string) error {
	dev, err := a.DeviceByID(userID, deviceID)
	if err != nil {
		return err
	}
	_, err = a.db.Exec(a.queries.updateDeviceConfig, config, time.Now().Unix(), dev.ID, userID)
	return err
}

// AckDeviceApplied records which config version the device has applied
// (axon#22): config_version > applied_version means the device still owes a
// sync. The version is stored as-reported; staleness is the signal.
func (a *Manager) AckDeviceApplied(userID, deviceID string, version int64) error {
	dev, err := a.DeviceByID(userID, deviceID)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	_, err = a.db.Exec(a.queries.updateDeviceApplied, version, now, now, dev.ID, userID)
	return err
}

// TouchDevice records a device sync for the status view.
func (a *Manager) TouchDevice(deviceID string) error {
	_, err := a.db.Exec(a.queries.updateDeviceLastSeen, time.Now().Unix(), deviceID)
	return err
}

// DeleteDevice removes a device AND revokes its token: revocation is instant,
// because every route that accepts the token now finds no device behind it and
// the token row is gone from user_token entirely.
func (a *Manager) DeleteDevice(userID, deviceID string) error {
	dev, err := a.DeviceByID(userID, deviceID)
	if err != nil {
		return err
	}
	return db.ExecTx(a.db, func(tx *sql.Tx) error {
		if dev.Token != "" {
			if _, err := tx.Exec(a.queries.deleteToken, userID, dev.Token); err != nil {
				return err
			}
		}
		_, err := tx.Exec(a.queries.deleteDevice, deviceID, userID)
		return err
	})
}

func (a *Manager) readDevice(rows *sql.Rows) (*Device, error) {
	var id, userID, token, label, config string
	var createdAt, updatedAt, lastSeen, configVersion, appliedVersion, appliedAt int64
	if !rows.Next() {
		return nil, ErrDeviceNotFound
	}
	if err := rows.Scan(&id, &userID, &token, &label, &config, &createdAt, &updatedAt, &lastSeen, &configVersion, &appliedVersion, &appliedAt); err != nil {
		return nil, err
	} else if err := rows.Err(); err != nil {
		return nil, err
	}
	return &Device{
		ID:        id,
		UserID:    userID,
		Token:     token,
		Label:     label,
		Config:    config,
		CreatedAt: time.Unix(createdAt, 0),
		UpdatedAt: time.Unix(updatedAt, 0),
		LastSeen:  time.Unix(lastSeen, 0),
		ConfigVersion:  configVersion,
		AppliedVersion: appliedVersion,
		AppliedAt:      time.Unix(appliedAt, 0),
	}, nil
}

// ClaimBuildKey provisions a device for the configured owner after verifying
// the build-time pairing key. This is the zero-input path for private app
// builds: the key is baked into the APK next to the server URL, so the app can
// pair itself on first launch — no code, no tap, no helper privileges. The
// resulting device and token are identical to code-paired ones (device scope,
// revocable, counted against the device budget).
func (a *Manager) ClaimBuildKey(ownerUsername, label string, origin netip.Addr) (*Device, string, error) {
	u, err := a.User(ownerUsername)
	if err != nil {
		return nil, "", err
	}
	var count int
	if err := a.db.QueryRow(a.queries.selectDeviceCount, u.ID).Scan(&count); err != nil {
		return nil, "", err
	} else if count >= deviceMaxPerUser {
		return nil, "", ErrTooManyDevices
	}
	now := time.Now()
	deviceID := util.RandomStringPrefix(deviceIDPrefix, deviceIDLength+len(deviceIDPrefix))
	if label == "" {
		label = "device " + deviceID[len(deviceIDPrefix):]
	}
	token := GenerateToken()
	err = db.ExecTx(a.db, func(tx *sql.Tx) error {
		if _, err := tx.Exec(a.queries.insertDevice, deviceID, u.ID, token, label, now.Unix(), now.Unix(), now.Unix()); err != nil {
			return err
		}
		_, err := tx.Exec(a.queries.upsertToken, u.ID, token, "device "+deviceID, now.Unix(), origin.String(), 0, false, TokenScopeDevice)
		return err
	})
	if err != nil {
		return nil, "", err
	}
	return &Device{ID: deviceID, UserID: u.ID, Token: token, Label: label, CreatedAt: now, UpdatedAt: now}, token, nil
}
